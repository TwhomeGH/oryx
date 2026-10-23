// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/go-redis/redis/v8"
)

// redisOpCounter records which commands touch which logical key family, so the diagnostics panel
// can show "hget:SRS_FORWARD_CONFIG 1200" instead of a bare command total.
type redisOpCounter struct {
	mu     sync.Mutex
	counts map[string]int64
}

var redisOpStats = &redisOpCounter{counts: make(map[string]int64)}

// keyFamily collapses a concrete key to its logical family: SRS_* constants keep their name,
// stream URLs collapse to their app segment, and other keys use their leading path segment.
func keyFamily(key string) string {
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "SRS_") {
		if i := strings.IndexAny(key, "/:_"); i > 0 {
			return key[:i]
		}
		return key
	}
	if i := strings.IndexByte(key, '/'); i > 0 {
		return key[:i]
	}
	return key
}

func (v *redisOpCounter) record(cmd string, args []interface{}) {
	if len(args) < 2 {
		return
	}
	key, ok := args[1].(string)
	if !ok {
		return
	}
	name := strings.ToLower(cmd) + ":" + keyFamily(key)
	v.mu.Lock()
	v.counts[name]++
	v.mu.Unlock()
}

// Top returns the most-called (command:family) entries, highest first.
func (v *redisOpCounter) Top(limit int) []map[string]interface{} {
	v.mu.Lock()
	type entry struct {
		name  string
		count int64
	}
	list := make([]entry, 0, len(v.counts))
	for k, c := range v.counts {
		list = append(list, entry{k, c})
	}
	v.mu.Unlock()

	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].name < list[j].name
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]interface{}{"name": e.name, "count": e.count})
	}
	return out
}

// redisStatsHook implements redis.Hook (v8) to count operations without changing behaviour.
type redisStatsHook struct{}

func (redisStatsHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (redisStatsHook) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	if args := cmd.Args(); len(args) > 1 {
		name, _ := args[0].(string)
		redisOpStats.record(name, args)
	}
	return nil
}

func (redisStatsHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (redisStatsHook) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	for _, cmd := range cmds {
		if args := cmd.Args(); len(args) > 1 {
			name, _ := args[0].(string)
			redisOpStats.record(name, args)
		}
	}
	return nil
}
