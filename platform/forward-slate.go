// Copyright (c) 2022-2026 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"

	"github.com/go-redis/redis/v8"
)

// forwardSlateConfig is the operator-configurable fallback slate: the clock's timezone, the
// explanatory text shown below it, and an optional policy to stop a forward when the source has
// been offline for a while (so a permanently-ended live does not keep showing the slate forever).
type forwardSlateConfig struct {
	TZ                 string `json:"tz"`
	Text               string `json:"text"`
	OfflineStopEnabled bool   `json:"offlineStopEnabled"`
	OfflineStopMinutes int    `json:"offlineStopMinutes"`
}

// forwardSlateDefaultText is the default message; the slate is shown on a (usually temporary)
// source interruption that auto-recovers, so it asks viewers to wait rather than announcing an end.
const forwardSlateDefaultText = "直播來源中斷，正在嘗試恢復，請稍候…"

func defaultForwardSlateConfig() forwardSlateConfig {
	return forwardSlateConfig{
		TZ:                 "",
		Text:               forwardSlateDefaultText,
		OfflineStopEnabled: false,
		OfflineStopMinutes: 5,
	}
}

// forwardSlateConfigLoad reads the slate config from Redis, returning defaults when unset.
func forwardSlateConfigLoad(ctx context.Context) forwardSlateConfig {
	conf := defaultForwardSlateConfig()
	if b, err := rdb.Get(ctx, SRS_FORWARD_SLATE).Result(); err != nil && err != redis.Nil {
		logger.Wf(ctx, "ignore forward slate get err %+v", err)
	} else if err == nil && b != "" {
		if err = json.Unmarshal([]byte(b), &conf); err != nil {
			logger.Wf(ctx, "ignore forward slate unmarshal err %+v", err)
		}
	}
	if conf.OfflineStopMinutes <= 0 {
		conf.OfflineStopMinutes = 5
	}
	return conf
}

// forwardSlateConfigSave validates and persists the slate config.
func forwardSlateConfigSave(ctx context.Context, conf forwardSlateConfig) error {
	if conf.OfflineStopMinutes <= 0 {
		conf.OfflineStopMinutes = 5
	}
	if conf.OfflineStopMinutes > 24*60 {
		return errors.Errorf("invalid offlineStopMinutes=%v", conf.OfflineStopMinutes)
	}
	if len([]rune(conf.Text)) > 200 {
		return errors.Errorf("slate text too long (max 200 chars)")
	}
	b, err := json.Marshal(conf)
	if err != nil {
		return errors.Wrapf(err, "marshal slate")
	}
	if err = rdb.Set(ctx, SRS_FORWARD_SLATE, string(b), 0).Err(); err != nil && err != redis.Nil {
		return errors.Wrapf(err, "set %v", SRS_FORWARD_SLATE)
	}
	logger.Tf(ctx, "forward slate saved, tz=%v text=%v offlineStop=%v/%vmin",
		conf.TZ, conf.Text, conf.OfflineStopEnabled, conf.OfflineStopMinutes)
	return nil
}

// forwardSlateOfflineStop reports whether a forward should stop after the source has been offline
// long enough, and the duration, per the current config.
func forwardSlateOfflineStop(ctx context.Context) (bool, int) {
	conf := forwardSlateConfigLoad(ctx)
	if !conf.OfflineStopEnabled {
		return false, 0
	}
	return true, conf.OfflineStopMinutes
}

// escapeDrawtextValue escapes a user-supplied string for use inside a drawtext option value in a
// filtergraph (the two escaping levels: filtergraph special chars and the option value).
func escapeDrawtextValue(s string) string {
	r := []rune{}
	for _, c := range s {
		switch c {
		case '\\', ':', '\'', '%', ',', ';', '[', ']', '=':
			r = append(r, '\\', c)
		default:
			r = append(r, c)
		}
	}
	return string(r)
}

// forwardSlateTextDrawtext builds the drawtext that shows the operator's message, centered above
// the clock. It returns "null" when the text is empty (no overlay).
func forwardSlateTextDrawtext(text string) string {
	if text == "" {
		return "null"
	}
	font := forwardSlateFont()
	if font == "" {
		return "null"
	}
	// The message sits slightly above center; the clock (see forwardSlateDrawtext) below it.
	return fmt.Sprintf("drawtext=fontfile=%v:text='%v':fontcolor=white:fontsize=42:x=(w-text_w)/2:y=(h-text_h)/2-50",
		font, escapeDrawtextValue(text))
}
