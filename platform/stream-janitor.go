// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ossrs/go-oryx-lib/logger"
)

// streamJanitor removes "ghost" entries from SRS_STREAM_ACTIVE: when an FFmpeg publisher is killed
// (SIGKILL, container stop) SRS never fires on_unpublish, so the hash keeps a stream that no longer
// exists. Players and the diagnostics page then show a stream that never plays.
//
// The janitor periodically compares Redis with SRS's own stream list and deletes the missing keys.
type streamJanitor struct {
	interval time.Duration
}

// NewStreamJanitor runs the janitor on the given interval.
func NewStreamJanitor(interval time.Duration) *streamJanitor {
	return &streamJanitor{interval: interval}
}

func (v *streamJanitor) Start(ctx context.Context) error {
	go func() {
		ticker := time.NewTicker(v.interval)
		defer ticker.Stop()
		// Run once shortly after startup so a restart cleans up immediately.
		time.Sleep(5 * time.Second)
		v.sweep(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				v.sweep(ctx)
			}
		}
	}()
	return nil
}

func (v *streamJanitor) Close() error { return nil }

// liveStreams asks SRS for the streams it actually has.
func (v *streamJanitor) liveStreams(ctx context.Context) (map[string]bool, error) {
	host := os.Getenv("SRS_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%v:1985/api/v1/streams/", host)
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Streams []struct {
			Name    string `json:"name"`
			App     string `json:"app"`
			Stream  string `json:"stream"`
			Publish struct {
				Active bool `json:"active"`
			} `json:"publish"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(parsed.Streams))
	for _, s := range parsed.Streams {
		if s.Publish.Active {
			live[s.App+"/"+s.Stream] = true
		}
	}
	return live, nil
}

func (v *streamJanitor) sweep(ctx context.Context) {
	live, err := v.liveStreams(ctx)
	if err != nil {
		logger.Wf(ctx, "stream janitor: list SRS streams err %+v", err)
		return
	}

	active, err := rdb.HGetAll(ctx, SRS_STREAM_ACTIVE).Result()
	if err != nil {
		logger.Wf(ctx, "stream janitor: hgetall err %+v", err)
		return
	}

	removed := 0
	for key := range active {
		if live[key] {
			continue
		}
		// Keep program streams out of Redis entirely (they are internal), so a missing entry there
		// is never expected; treat everything else as ghost state.
		if err := rdb.HDel(ctx, SRS_STREAM_ACTIVE, key).Err(); err != nil {
			logger.Wf(ctx, "stream janitor: hdel %v err %+v", key, err)
			continue
		}
		removed++
		logger.Tf(ctx, "stream janitor: removed ghost stream %v", key)
	}
	if removed > 0 {
		logger.Tf(ctx, "stream janitor: cleaned %v ghost stream(s)", removed)
	}
}
