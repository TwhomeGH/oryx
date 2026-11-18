// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"
)

// A forward target's key is its identity: it appears in the API, in logs, in event timelines and in
// the Redis keys that hold metrics and sessions. Custom targets used to be renameable inside a
// built-in slot, which let the key (bilibili) disagree with what the operator sees (RStream) and made
// every diagnostic ambiguous. This migration moves such targets onto their own key, derived from the
// label, so internal and external identifiers are the same thing.
var forwardBuiltinPlatforms = []string{"wx", "bilibili", "kuaishou"}

var forwardSlugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// forwardSlug derives the key suffix from the operator-visible label.
func forwardSlug(label, fallback string) string {
	s := forwardSlugUnsafe.ReplaceAllString(strings.ToLower(label), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "custom"
	}
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

// migrateForwardCustomKeys rewrites every custom target that sits on a built-in key. It is
// idempotent: after the first run nothing matches and it returns immediately.
func migrateForwardCustomKeys(ctx context.Context) error {
	if rdb == nil {
		return nil
	}

	configs, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result()
	if err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
	}

	taken := map[string]bool{}
	for k := range configs {
		taken[k] = true
	}

	for key, raw := range configs {
		var conf ForwardConfigure
		if err := json.Unmarshal([]byte(raw), &conf); err != nil {
			logger.Wf(ctx, "forward migrate skip %v, unmarshal %+v", key, err)
			continue
		}
		if !conf.Customed || !slicesContains(forwardBuiltinPlatforms, key) {
			continue
		}

		base := "forwarding-" + forwardSlug(conf.Label, "custom")
		newKey := base
		for i := 2; taken[newKey]; i++ {
			newKey = fmt.Sprintf("%v-%v", base, i)
		}
		taken[newKey] = true

		conf.Platform = newKey
		b, err := json.Marshal(&conf)
		if err != nil {
			return errors.Wrapf(err, "marshal %v", newKey)
		}

		// Config first, then the per-key state, and only then drop the old key: a crash midway leaves
		// a duplicate rather than losing the target.
		if err := rdb.HSet(ctx, SRS_FORWARD_CONFIG, newKey, string(b)).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hset %v %v", SRS_FORWARD_CONFIG, newKey)
		}

		if task, err := rdb.HGet(ctx, SRS_FORWARD_TASK, key).Result(); err == nil && task != "" {
			if err := rdb.HSet(ctx, SRS_FORWARD_TASK, newKey, task).Err(); err != nil && err != redis.Nil {
				return errors.Wrapf(err, "hset %v task", SRS_FORWARD_TASK)
			}
			rdb.HDel(ctx, SRS_FORWARD_TASK, key)
		}

		froms := []string{
			fmt.Sprintf("%v:%v", SRS_FORWARD_METRICS, key),
			fmt.Sprintf("%v:%v", SRS_FORWARD_SESSIONS, key),
			fmt.Sprintf("%v_CURRENT:%v", SRS_FORWARD_SESSIONS, key),
		}
		tos := []string{
			fmt.Sprintf("%v:%v", SRS_FORWARD_METRICS, newKey),
			fmt.Sprintf("%v:%v", SRS_FORWARD_SESSIONS, newKey),
			fmt.Sprintf("%v_CURRENT:%v", SRS_FORWARD_SESSIONS, newKey),
		}
		for i := range froms {
			if exists, err := rdb.Exists(ctx, froms[i]).Result(); err == nil && exists > 0 {
				if err := rdb.Rename(ctx, froms[i], tos[i]).Err(); err != nil && err != redis.Nil {
					logger.Wf(ctx, "forward migrate rename %v -> %v err %+v", froms[i], tos[i], err)
				}
			}
		}

		fromDir := filepath.Join(forwardTimingRoot(), key)
		toDir := filepath.Join(forwardTimingRoot(), newKey)
		if _, err := os.Stat(fromDir); err == nil {
			if err := os.Rename(fromDir, toDir); err != nil {
				logger.Wf(ctx, "forward migrate timing dir %v -> %v err %+v", fromDir, toDir, err)
			}
		}

		if err := rdb.HDel(ctx, SRS_FORWARD_CONFIG, key).Err(); err != nil && err != redis.Nil {
			return errors.Wrapf(err, "hdel %v %v", SRS_FORWARD_CONFIG, key)
		}

		logger.Tf(ctx, "forward migrated custom target %v -> %v (label=%v, enabled=%v)",
			key, newKey, conf.Label, conf.Enabled)
	}

	return normalizeForwardEndState(ctx)
}

// normalizeForwardEndState clears a stale "ended" flag on a target that is already disabled. Before
// the end flow was fixed, pressing 結束直播 set ended=true and disabled the target immediately, and
// nothing ever cleared the flag, so the UI kept offering 取消結束 for a target that had already
// stopped.
func normalizeForwardEndState(ctx context.Context) error {
	configs, err := rdb.HGetAll(ctx, SRS_FORWARD_CONFIG).Result()
	if err != nil && err != redis.Nil {
		return errors.Wrapf(err, "hgetall %v", SRS_FORWARD_CONFIG)
	}
	for key, raw := range configs {
		var conf ForwardConfigure
		if err := json.Unmarshal([]byte(raw), &conf); err != nil {
			continue
		}
		if conf.Enabled || !conf.Ended {
			continue
		}
		conf.Ended = false
		conf.EndedAt = 0
		b, err := json.Marshal(&conf)
		if err != nil {
			continue
		}
		if err := rdb.HSet(ctx, SRS_FORWARD_CONFIG, key, string(b)).Err(); err != nil && err != redis.Nil {
			logger.Wf(ctx, "forward normalize ended state %v err %+v", key, err)
			continue
		}
		logger.Tf(ctx, "forward cleared stale ended flag, platform=%v", key)
	}
	return nil
}
