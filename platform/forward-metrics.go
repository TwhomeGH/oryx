// Copyright (c) 2013-2025, SRS and All rights reserved.
// The forward quality metrics, parsed from the fanout FFmpeg progress line and the program
// relay reconnect evidence. These are LOCAL processing observations; they never assert that
// the destination platform accepted or is playing the stream.

package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ForwardMetrics is a snapshot of the fanout FFmpeg output progress plus the source reconnect
// evidence of the program relay.
type ForwardMetrics struct {
	// Output progress, from the fanout FFmpeg progress line.
	Frame       int64   `json:"frame,omitempty"`
	OutputFPS   float64 `json:"outputFps,omitempty"`
	AvgFPS      float64 `json:"avgFps,omitempty"`
	Speed       float64 `json:"speed,omitempty"`
	BitrateKbps float64 `json:"bitrateKbps,omitempty"`
	SizeBytes   int64   `json:"sizeBytes,omitempty"`
	MediaTime   string  `json:"mediaTime,omitempty"`
	MediaSecond float64 `json:"mediaSeconds,omitempty"`

	// Seconds since the last output progress line. Grows while the fanout holds the last frame
	// (source offline); reset to ~0 once output resumes.
	NoOutputSeconds float64 `json:"noOutputSeconds,omitempty"`
	// The output media clock ahead of the wall clock since the process started. A sustained
	// positive drift indicates timestamp jumps in the input (e.g. after a relay reconnect).
	MediaClockDrift float64 `json:"mediaClockDriftSec,omitempty"`

	// Source reconnect evidence, from the program relay FFmpeg stderr.
	RelayReconnects int    `json:"relayReconnects,omitempty"`
	LastReconnectAt string `json:"lastReconnectAt,omitempty"`
	ReconnectReason string `json:"reconnectReason,omitempty"`
}

// forwardProgress is the last parsed fanout FFmpeg progress line.
type forwardProgress struct {
	valid       bool
	frame       int64
	fps         float64
	sizeBytes   int64
	mediaSec    float64
	mediaTime   string
	bitrateKbps float64
	speed       float64
	at          time.Time
}

// ffmpegProgressTokens extracts "key=value" tokens from a normalized FFmpeg progress line, such
// as "frame=180 fps=90 q=-1.0 size=1024kB time=00:00:03.00 bitrate=2796.2kbits/s speed=1.5x".
var ffmpegProgressTokens = regexp.MustCompile(`(?:^|\s)(frame|fps|size|time|bitrate|speed)=\s*(\S+)`)

// parseFFmpegProgress parses one FFmpeg progress line. It returns false when the line does not
// carry a usable media time and speed (e.g. "time=N/A"), so unavailable values never look like 0.
func parseFFmpegProgress(log string) (forwardProgress, bool) {
	var p forwardProgress

	tokens := map[string]string{}
	for _, m := range ffmpegProgressTokens.FindAllStringSubmatch(log, -1) {
		tokens[m[1]] = m[2]
	}

	sec, ok := parseFFmpegTimestamp(tokens["time"])
	if !ok {
		return p, false
	}
	speed, err := strconv.ParseFloat(strings.TrimSuffix(tokens["speed"], "x"), 64)
	if err != nil {
		return p, false
	}

	p.mediaTime, p.mediaSec, p.speed, p.valid = tokens["time"], sec, speed, true

	if f, err := strconv.ParseInt(tokens["frame"], 10, 64); err == nil {
		p.frame = f
	}
	if f, err := strconv.ParseFloat(tokens["fps"], 64); err == nil {
		p.fps = f
	}
	if br := strings.TrimSuffix(tokens["bitrate"], "kbits/s"); br != "" {
		if f, err := strconv.ParseFloat(br, 64); err == nil {
			p.bitrateKbps = f
		}
	}
	p.sizeBytes = parseFFmpegSize(tokens["size"])

	return p, true
}

// parseFFmpegTimestamp parses "HH:MM:SS.cc" into seconds.
func parseFFmpegTimestamp(s string) (float64, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.ParseFloat(parts[0], 64)
	m, err2 := strconv.ParseFloat(parts[1], 64)
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return h*3600 + m*60 + sec, true
}

// parseFFmpegSize parses an FFmpeg size token like "1024kB" or "3.5MiB" into bytes.
func parseFFmpegSize(s string) int64 {
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}

	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := s[i:]

	mult := float64(1)
	switch unit {
	case "kB":
		mult = 1e3
	case "KiB":
		mult = 1024
	case "MB":
		mult = 1e6
	case "MiB":
		mult = 1 << 20
	case "GB":
		mult = 1e9
	case "GiB":
		mult = 1 << 30
	}
	return int64(f * mult)
}
