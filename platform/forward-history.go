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
	sessions := make([]ForwardSession, 0, len(raw))
	for _, item := range raw {
		var session ForwardSession
		if err := json.Unmarshal([]byte(item), &session); err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
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
