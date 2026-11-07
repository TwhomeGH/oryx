// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/ossrs/go-oryx-lib/errors"
)

// forwardSessionHistorySize bounds how many past runs are retained per target. An operator needs
// the previous few runs to compare a failure with the run before it; keeping every run forever
// would grow without bound in the in-memory store.
const forwardSessionHistorySize = 10

// ForwardSession is one completed run of a forward target, persisted so a failure can be
// investigated after the process has restarted or the task has moved on.
type ForwardSession struct {
	ID           string         `json:"id"`
	Running      bool           `json:"running,omitempty"`
	StartedAt    string         `json:"startedAt"`
	EndedAt      string         `json:"endedAt"`
	DurationSec  float64        `json:"durationSec"`
	FinalState   string         `json:"finalState"`
	RestartCount int            `json:"restartCount"`
	LastError    string         `json:"lastError,omitempty"`
	Events       []ForwardEvent `json:"events"`
	Logs         []string       `json:"logs"`
}

// forwardSessionKey returns the Redis list key holding a target's run history.
func forwardSessionKey(platform string) string {
	return fmt.Sprintf("%v:%v", SRS_FORWARD_SESSIONS, platform)
}

// forwardSessionCurrentKey holds the live snapshot of the run in progress. It exists so the
// diagnostics page can show the current session and so a crash/restart does not erase the record
// of a run that never got to finish.
func forwardSessionCurrentKey(platform string) string {
	return fmt.Sprintf("%v_CURRENT:%v", SRS_FORWARD_SESSIONS, platform)
}

// saveForwardSession appends a completed run to the bounded per-target history. Diagnostics are a
// best-effort side channel: a failure to persist must never break forwarding, so the error is only
// logged.
func saveForwardSession(ctx context.Context, platform string, session ForwardSession) {
	if rdb == nil {
		return
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return
	}
	key := forwardSessionKey(platform)
	if err := rdb.LPush(ctx, key, string(raw)).Err(); err != nil && err != redis.Nil {
		return
	}
	// Keep one extra so a concurrent read during the trim never sees an empty list.
	_ = rdb.LTrim(ctx, key, 0, forwardSessionHistorySize-1).Err()
}

// queryForwardSessions returns the stored run history of a target, newest first.
func queryForwardSessions(ctx context.Context, platform string) ([]ForwardSession, error) {
	if rdb == nil {
		return nil, errors.New("redis is not ready")
	}
	raw, err := rdb.LRange(ctx, forwardSessionKey(platform), 0, forwardSessionHistorySize-1).Result()
	if err != nil && err != redis.Nil {
		return nil, errors.Wrapf(err, "lrange %v", forwardSessionKey(platform))
	}
	sessions := make([]ForwardSession, 0, len(raw)+1)
	// The running session is not in the history list yet (it is appended when the run ends), so
	// prepend its latest checkpoint; otherwise the panel reads "no completed runs" while a target
	// is actively forwarding.
	if cur, err := rdb.Get(ctx, forwardSessionCurrentKey(platform)).Result(); err == nil && cur != "" {
		var session ForwardSession
		if json.Unmarshal([]byte(cur), &session) == nil {
			session.Running = true
			sessions = append(sessions, session)
		}
	} else if err != nil && err != redis.Nil {
		return nil, errors.Wrapf(err, "get %v", forwardSessionCurrentKey(platform))
	}
	for _, item := range raw {
		var session ForwardSession
		if err := json.Unmarshal([]byte(item), &session); err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// saveForwardSessionCurrent checkpoints the in-progress run, refreshed periodically by the task.
func saveForwardSessionCurrent(ctx context.Context, platform string, session ForwardSession, ttl time.Duration) {
	if rdb == nil {
		return
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return
	}
	_ = rdb.Set(ctx, forwardSessionCurrentKey(platform), string(raw), ttl).Err()
}

// clearForwardSessionCurrent drops the live checkpoint once the run has been recorded.
func clearForwardSessionCurrent(ctx context.Context, platform string) {
	if rdb == nil {
		return
	}
	_ = rdb.Del(ctx, forwardSessionCurrentKey(platform)).Err()
}

// buildForwardSession snapshots the live task into a history record, used when a run ends.
func (v *ForwardTask) buildForwardSession(startedAt time.Time) ForwardSession {
	v.lock.Lock()
	defer v.lock.Unlock()

	ended := time.Now()
	session := ForwardSession{
		ID:           fmt.Sprintf("%v-%v", v.Platform, startedAt.Unix()),
		StartedAt:    startedAt.Format(time.RFC3339),
		EndedAt:      ended.Format(time.RFC3339),
		DurationSec:  ended.Sub(startedAt).Seconds(),
		FinalState:   v.State,
		RestartCount: v.RestartCount,
		LastError:    v.LastError,
	}
	session.Events = append([]ForwardEvent(nil), v.events...)
	session.Logs = append([]string(nil), v.logs...)
	return session
}
