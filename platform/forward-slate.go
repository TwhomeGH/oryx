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
	// KeyframeSeconds is the target GOP duration used by normalize/composite transcoding.
	KeyframeSeconds int `json:"keyframeSeconds"`
	// OutputBitrateMbps caps the transcoded output bitrate (normalize/composite), in Mbps.
	OutputBitrateMbps int `json:"outputBitrateMbps"`
	// Normalize transcodes the shared program once (CFR + timestamps) so every target gets the
	// same stream with a single encode; targets then always -c copy.
	Normalize bool `json:"normalize"`
	// OutputCQ is the quality target (0-51, lower is better); used as -cq (NVENC) / -crf (x264).
	OutputCQ int `json:"outputCQ"`
	// OutputPresets maps an encoder kind (nvenc, x264, amf, qsv) to its preset, so switching
	// encoders never applies one encoder's vocabulary to another.
	OutputPresets map[string]string `json:"outputPresets"`
	// OutputTunes maps an encoder kind to its -tune value ("none"/"" means omit it); the value
	// sets differ between NVENC (hq/ll/ull/lossless) and x264 (film/animation/grain/...).
	OutputTunes map[string]string `json:"outputTunes"`
	// EndText is shown when the operator manually ends the live (not a source outage).
	EndText string `json:"endText"`
	// EndGraceSeconds is how long the end screen can still be cancelled before the target stops.
	EndGraceSeconds int `json:"endGraceSeconds"`
}

// forwardSlateDefaultText is the default message; the slate is shown on a (usually temporary)
// source interruption that auto-recovers, so it asks viewers to wait rather than announcing an end.
const forwardSlateDefaultText = "直播來源中斷，正在嘗試恢復，請稍候…"

// forwardSlateDefaultEndText is shown after a deliberate "end live", so viewers see a proper
// end rather than the outage message while the 60s grace runs.
const forwardSlateDefaultEndText = "直播已結束，謝謝收看"

func defaultForwardSlateConfig() forwardSlateConfig {
	return forwardSlateConfig{
		TZ:                 "",
		Text:               forwardSlateDefaultText,
		OfflineStopEnabled: true,
		OfflineStopMinutes: 5,
		KeyframeSeconds:    2,
		OutputBitrateMbps:  6,
		Normalize:          true,
		OutputCQ:           21,
		OutputPresets:      map[string]string{"nvenc": "p4", "x264": "veryfast"},
		EndText:            forwardSlateDefaultEndText,
		EndGraceSeconds:    60,
	}
}

// forwardSlateConfigLoad reads the slate config from Redis, returning defaults when unset.
func forwardSlateConfigLoad(ctx context.Context) forwardSlateConfig {
	conf := defaultForwardSlateConfig()
	if rdb == nil {
		return conf
	}
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
	if conf.KeyframeSeconds <= 0 || conf.KeyframeSeconds > 10 {
		conf.KeyframeSeconds = 2
	}
	if conf.OutputBitrateMbps <= 0 || conf.OutputBitrateMbps > 50 {
		conf.OutputBitrateMbps = 6
	}
	if conf.OutputCQ <= 0 || conf.OutputCQ > 51 {
		conf.OutputCQ = 21
	}
	if conf.OutputPresets == nil {
		conf.OutputPresets = map[string]string{}
	}
	if conf.OutputTunes == nil {
		conf.OutputTunes = map[string]string{}
	}
	if conf.OutputPresets["nvenc"] == "" {
		conf.OutputPresets["nvenc"] = "p4"
	}
	if conf.OutputPresets["x264"] == "" {
		conf.OutputPresets["x264"] = "veryfast"
	}
	if conf.EndText == "" {
		conf.EndText = forwardSlateDefaultEndText
	}
	if conf.EndGraceSeconds <= 0 || conf.EndGraceSeconds > 600 {
		conf.EndGraceSeconds = 60
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
	if conf.KeyframeSeconds <= 0 {
		conf.KeyframeSeconds = 2
	}
	if conf.KeyframeSeconds > 10 {
		return errors.Errorf("invalid keyframeSeconds=%v", conf.KeyframeSeconds)
	}
	if conf.OutputBitrateMbps <= 0 {
		conf.OutputBitrateMbps = 6
	}
	if conf.OutputBitrateMbps > 50 {
		return errors.Errorf("invalid outputBitrateMbps=%v", conf.OutputBitrateMbps)
	}
	if conf.OutputCQ <= 0 {
		conf.OutputCQ = 21
	}
	if conf.OutputCQ > 51 {
		return errors.Errorf("invalid outputCQ=%v", conf.OutputCQ)
	}
	for k, v := range conf.OutputPresets {
		switch k {
		case "nvenc", "x264", "amf", "qsv":
		default:
			return errors.Errorf("invalid encoder kind=%v", k)
		}
		if v == "" || len(v) > 16 {
			return errors.Errorf("invalid preset for %v=%v", k, v)
		}
	}
	for k, v := range conf.OutputTunes {
		switch k {
		case "nvenc", "x264", "amf", "qsv":
		default:
			return errors.Errorf("invalid encoder kind=%v", k)
		}
		if len(v) > 16 {
			return errors.Errorf("invalid tune for %v=%v", k, v)
		}
	}
	if conf.EndGraceSeconds <= 0 {
		conf.EndGraceSeconds = 60
	}
	if conf.EndGraceSeconds > 600 {
		return errors.Errorf("invalid endGraceSeconds=%v", conf.EndGraceSeconds)
	}
	if len([]rune(conf.EndText)) > 200 {
		return errors.Errorf("end text too long (max 200 chars)")
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
