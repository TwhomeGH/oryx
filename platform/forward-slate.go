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
	// Normalize keeps the historical on/off switch; NormalizeMode is the finer control:
	//   auto        - copy a CFR source, preserve a stable VFR source, CFR only when forced
	//   cfr         - always transcode to constant frame rate (compatibility, duplicates frames)
	//   passthrough - repair timestamps only, keep the source cadence (no -r, no re-encode)
	//   off         - plain copy
	// CFR is lossy: duplicate frames are inserted and the original VFR timing cannot be recovered
	// downstream, so "preserve" is the default for VFR sources.
	Normalize     bool   `json:"normalize"`
	NormalizeMode string `json:"normalizeMode"`
	// OutputCQ is the quality target (0-51, lower is better); used as -cq (NVENC) / -crf (x264).
	OutputCQ int `json:"outputCQ"`
	// OutputPresets maps an encoder kind (nvenc, x264, amf, qsv) to its preset, so switching
	// encoders never applies one encoder's vocabulary to another.
	OutputPresets map[string]string `json:"outputPresets"`
	// OutputRateMode controls the frame rate of the paths that re-encode (composite/slate and
	// forced CFR). "auto" follows the source's nominal rate; "fixed" uses OutputRate. Copy and
	// repair are unaffected: they never set -r and keep the source cadence.
	OutputRateMode string `json:"outputRateMode"`
	// OutputRate is the fixed output frame rate; 0 means unset. Decimals are allowed (29.97).
	OutputRate float64 `json:"outputRate"`
	// OutputTunes maps an encoder kind to its -tune value ("none"/"" means omit it); the value
	// sets differ between NVENC (hq/ll/ull/lossless) and x264 (film/animation/grain/...).
	OutputTunes map[string]string `json:"outputTunes"`
	// EndText is shown when the operator manually ends the live (not a source outage).
	EndText string `json:"endText"`
	// EndGraceSeconds is how long the end screen can still be cancelled before the target stops.
	EndGraceSeconds int `json:"endGraceSeconds"`
	// EndGraceMigrated marks that the old 60s default was replaced, so the migration runs once
	// and never overrides a value the operator chose afterwards.
	EndGraceMigrated bool `json:"endGraceMigrated"`
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
		OutputRateMode:     "auto",
		OutputBitrateMbps:  6,
		Normalize:          true,
		NormalizeMode:      "auto",
		OutputCQ:           21,
		OutputPresets:      map[string]string{"nvenc": "p4", "x264": "veryfast"},
		EndText:            forwardSlateDefaultEndText,
		EndGraceSeconds:    30,
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
	// One-time migration of the old 60s default. Doing it on load (and persisting) is what makes it
	// apply to an install that already stored the previous default.
	if !conf.EndGraceMigrated && conf.EndGraceSeconds == 60 {
		conf.EndGraceSeconds = 30
		conf.EndGraceMigrated = true
		if err := forwardSlateConfigSave(ctx, conf); err != nil {
			logger.Wf(ctx, "ignore forward slate migration save err %+v", err)
		} else {
			logger.Tf(ctx, "forward slate migrated endGraceSeconds 60->30")
		}
	}
	if conf.OutputRateMode == "" {
		conf.OutputRateMode = "auto"
	}
	if conf.OutputRateMode != "auto" && conf.OutputRateMode != "fixed" {
		conf.OutputRateMode = "auto"
	}
	if conf.OutputRate < 1 || conf.OutputRate > 240 {
		conf.OutputRate = 0
	}
	// A fixed mode without a usable value would silently fall back to auto; make it explicit.
	if conf.OutputRateMode == "fixed" && conf.OutputRate == 0 {
		conf.OutputRateMode = "auto"
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
	// Backward compatibility: older configs only carry the boolean.
	switch conf.NormalizeMode {
	case "auto", "cfr", "passthrough", "off":
	default:
		if conf.Normalize {
			conf.NormalizeMode = "auto"
		} else {
			conf.NormalizeMode = "off"
		}
	}
	conf.Normalize = conf.NormalizeMode != "off"
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
		conf.EndGraceSeconds = 30
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
	switch conf.NormalizeMode {
	case "", "auto", "cfr", "passthrough", "off":
	default:
		return errors.Errorf("invalid normalizeMode=%v", conf.NormalizeMode)
	}
	if conf.NormalizeMode == "" {
		conf.NormalizeMode = "auto"
	}
	conf.Normalize = conf.NormalizeMode != "off"
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
		conf.EndGraceSeconds = 30
	}
	conf.EndGraceMigrated = true
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

// forwardEndOverlayText is what the top-layer text shows: the end message once the stream is
// ending, nothing otherwise.
func forwardEndOverlayText(conf forwardSlateConfig, ended bool) string {
	if !ended {
		return ""
	}
	return conf.EndText
}

// forwardEndDrawtext draws the end-screen message ABOVE the source picture. The slate text sits on
// the base layer and is covered by the source overlay whenever the source is alive, so ending a live
// stream would otherwise change nothing on screen until the target stopped.
func forwardEndDrawtext(text string) string {
	font := forwardSlateFont()
	if font == "" {
		return "null"
	}
	return fmt.Sprintf("drawtext@end=fontfile=%v:text='%v':fontcolor=white:fontsize=h/12:box=1:boxcolor=black@0.55:boxborderw=(h/24):x=(w-text_w)/2:y=(h-text_h)/2",
		font, escapeDrawtextValue(text))
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
	return fmt.Sprintf("drawtext@slate=fontfile=%v:text='%v':fontcolor=white:fontsize=42:x=(w-text_w)/2:y=(h-text_h)/2-50",
		font, escapeDrawtextValue(text))
}
