// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// recordStallGraceSec is how long past the worker's own finalize window a recording may sit before
// it is reported as stalled. The DVR worker keeps a recording open for a while after the last
// segment (30s, or 300s when recording all streams in production) and then finalizes it; flagging
// anything earlier would call every normal end-of-stream a fault.
const recordStallGraceSec = 60

// recordExpirySeconds mirrors RecordM3u8Stream.expired(), so the diagnostics agree with the worker
// instead of inventing a second, conflicting definition of "should have finished by now".
func recordExpirySeconds(ctx context.Context) int64 {
	enabled := false
	if all, err := rdb.HGet(ctx, SRS_RECORD_PATTERNS, "all").Result(); err == nil {
		enabled = all == "true"
	}
	if enabled && envNodeEnv() != "development" {
		return 300
	}
	return 30
}

// recordDiagEntry is one recording, whether still running or finished. The fields mirror what the
// DVR worker persists in Redis; nothing here is guessed.
type recordDiagEntry struct {
	// Identity.
	UUID    string `json:"uuid"`
	M3u8URL string `json:"m3u8Url"`
	App     string `json:"app,omitempty"`
	Stream  string `json:"stream,omitempty"`

	// Whether the recording is still accumulating segments.
	Recording bool `json:"recording"`
	// SRS's post-processing flag (remuxing the ts segments into index.mp4).
	Processing bool `json:"processing"`
	// Marked for deletion by the operator.
	Expired bool `json:"expired"`

	// Segment count and timestamps, as strings in RFC3339 (SRS may omit done until finished).
	Segments    int    `json:"segments"`
	Update      string `json:"update,omitempty"`
	Done        string `json:"done,omitempty"`
	AgeSec      int64  `json:"ageSec,omitempty"`
	DurationSec int64  `json:"durationSec,omitempty"`

	// Output file, when it exists.
	File        string `json:"file,omitempty"`
	SizeBytes   int64  `json:"sizeBytes"`
	MissingFile bool   `json:"missingFile"`

	// Health state: recording | done | stalled | processing | expired | missing.
	State string `json:"state"`
	// Why the state was chosen, shown next to it so a label never appears without a reason.
	StateReason string `json:"stateReason,omitempty"`
}

// recordDiagConfig is what the DVR worker was told to record.
type recordDiagConfig struct {
	All          bool     `json:"all"`
	Globs        []string `json:"globs"`
	ProcessCpDir string   `json:"processCpDir"`
}

// recordFileSize returns the size of the recording's output file and whether it exists.
func recordFileSize(uuid string) (int64, bool) {
	if uuid == "" {
		return 0, false
	}
	path := fmt.Sprintf("%v/%v/index.mp4", recordHome, uuid)
	if st, err := os.Stat(path); err != nil {
		return 0, false
	} else {
		return st.Size(), true
	}
}

// recordDirBytes reports how much disk a recording occupies while it is still running, where the
// only output so far is the ts segments (index.mp4 appears after finalize).
func recordDirStats(uuid string) (bytes int64, files int, ok bool) {
	if uuid == "" {
		return 0, 0, false
	}
	dir := fmt.Sprintf("%v/%v", recordHome, uuid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files++
		if info, err := entry.Info(); err == nil {
			bytes += info.Size()
		}
	}
	return bytes, files, true
}

// formatRecordTime keeps the SRS timestamp when it is present and parseable, so the UI renders it
// in the viewer's timezone instead of a server-side local string.
func parseRecordTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// classifyRecord derives the state and its reason from the persisted fields. Labels must always be
// backed by an observable fact, so every branch states what it saw.
func classifyRecord(e *recordDiagEntry, now time.Time, expirySec int64) {
	switch {
	case e.Expired:
		e.State, e.StateReason = "expired", "marked for deletion"
		return
	case e.Processing:
		e.State, e.StateReason = "processing", "post-processing into index.mp4"
		return
	case e.Recording:
		if t, ok := parseRecordTime(e.Update); ok {
			e.AgeSec = int64(now.Sub(t).Seconds())
			if e.AgeSec >= expirySec+recordStallGraceSec {
				e.State = "stalled"
				e.StateReason = fmt.Sprintf("no new segment for %ds, past the %ds finalize window", e.AgeSec, expirySec)
				return
			}
			if e.AgeSec >= expirySec {
				// The source stopped; the worker is within its own window and will finalize on its own.
				e.State = "finalizing"
				e.StateReason = fmt.Sprintf("waiting to finalize (window %ds)", expirySec)
				return
			}
			e.State, e.StateReason = "recording", fmt.Sprintf("last segment %ds ago", e.AgeSec)
			return
		}
		e.State, e.StateReason = "recording", "segments arriving"
		return
	}

	// Not recording and not processing: the run is finished as far as SRS is concerned.
	if e.MissingFile {
		e.State, e.StateReason = "missing", "output file not found"
		return
	}
	e.State, e.StateReason = "done", "finalized"
}

// loadRecordEntries reads both the in-flight and the finished recordings from Redis. Failures are
// reported rather than silently skipped, so an empty list never hides a broken worker.
func loadRecordEntries(ctx context.Context) (working []*recordDiagEntry, finished []*recordDiagEntry, r0 error) {
	now := time.Now()
	expirySec := recordExpirySeconds(ctx)

	if raw, err := rdb.HGetAll(ctx, SRS_RECORD_M3U8_WORKING).Result(); err != nil && err != redis.Nil {
		return nil, nil, errors.Wrapf(err, "hgetall %v", SRS_RECORD_M3U8_WORKING)
	} else {
		for _, v := range raw {
			var stream RecordM3u8Stream
			if err := json.Unmarshal([]byte(v), &stream); err != nil {
				continue
			}
			e := &recordDiagEntry{
				UUID: stream.UUID, M3u8URL: stream.M3u8URL, Recording: stream.Done == "",
				Segments: stream.NN, Update: stream.Update, Done: stream.Done, Expired: stream.Expired,
			}
			// While recording there is no index.mp4 yet; the segments themselves are the size that
			// matters, and claiming "file missing" during a healthy recording would be alarming.
			if size, files, ok := recordDirStats(e.UUID); ok {
				e.SizeBytes = size
				// The worker's segment counter lags while the session is live, so report what is
				// actually on disk instead of a misleading 0.
				if e.Segments == 0 && files > 0 {
					e.Segments = files
				}
			} else {
				e.MissingFile = true
			}
			classifyRecord(e, now, expirySec)
			working = append(working, e)
		}
	}

	if raw, err := rdb.HGetAll(ctx, SRS_RECORD_M3U8_ARTIFACT).Result(); err != nil && err != redis.Nil {
		return nil, nil, errors.Wrapf(err, "hgetall %v", SRS_RECORD_M3U8_ARTIFACT)
	} else {
		for _, v := range raw {
			var artifact M3u8VoDArtifact
			if err := json.Unmarshal([]byte(v), &artifact); err != nil {
				continue
			}
			e := &recordDiagEntry{
				UUID: artifact.UUID, M3u8URL: artifact.M3u8URL, App: artifact.App, Stream: artifact.Stream,
				Processing: artifact.Processing, Segments: artifact.NN, Update: artifact.Update, Done: artifact.Done,
			}
			if size, ok := recordFileSize(e.UUID); ok {
				e.SizeBytes = size
				e.File = fmt.Sprintf("%v/%v/index.mp4", recordHome, e.UUID)
			} else {
				e.MissingFile = true
			}
			// The artifact's update/done timestamps are bookkeeping moments (they can be seconds
			// apart even for an hour-long recording); the real length is the sum of the segments.
			var total float64
			for _, f := range artifact.Files {
				total += f.Duration
			}
			e.DurationSec = int64(total)
			classifyRecord(e, now, expirySec)
			finished = append(finished, e)
		}
	}

	sort.Slice(finished, func(i, j int) bool { return finished[i].Done > finished[j].Done })
	if len(finished) > 20 {
		finished = finished[:20]
	}
	return working, finished, nil
}

// handleMgmtRecordQuery reports the DVR state: what is recording now, what finished, and why a
// recording looks unhealthy. Recording is event-driven (SRS on_hls), not a task with retries, so
// the states are recording/done/stalled/processing/expired/missing rather than forward-like ones.
func handleMgmtRecordQuery(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/record/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			conf := recordDiagConfig{}
			if all, err := rdb.HGet(ctx, SRS_RECORD_PATTERNS, "all").Result(); err == nil {
				conf.All = all == "true"
			}
			if globs, err := rdb.HGet(ctx, SRS_RECORD_PATTERNS, "globs").Result(); err == nil && globs != "" {
				if err := json.Unmarshal([]byte(globs), &conf.Globs); err != nil {
					return errors.Wrapf(err, "parse globs %v", globs)
				}
			}
			if dir, err := rdb.HGet(ctx, SRS_RECORD_PATTERNS, string(RecordPostProcessCpFile)).Result(); err == nil {
				conf.ProcessCpDir = dir
			}

			working, finished, err := loadRecordEntries(ctx)
			if err != nil {
				return err
			}

			// A recording that is still active also has an artifact row (with processing=true), so
			// the same uuid would be listed twice and look like a finished run. The working entry
			// is the truth while the session is live; keep only artifacts whose session ended.
			workingUUIDs := make(map[string]bool, len(working))
			for _, e := range working {
				workingUUIDs[e.UUID] = true
			}
			filtered := finished[:0]
			for _, e := range finished {
				if !workingUUIDs[e.UUID] {
					filtered = append(filtered, e)
				}
			}
			finished = filtered

			// Recording is driven by SRS on_hls, so with HLS disabled no segment event ever
			// arrives and the feature is silently inert. Report that explicitly rather than
			// showing an empty list that looks like "nothing is being recorded right now".
			hlsEnabled := true
			if v, err := rdb.HGet(ctx, SRS_HLS_MASTER, "enabled").Result(); err == nil && v == "false" {
				hlsEnabled = false
			}
			enabled := conf.All || len(conf.Globs) > 0
			blocked := ""
			// Only warn about the HLS dependency when nothing is being recorded: if segments are
			// arriving, the dependency is evidently satisfied and the warning would contradict
			// the evidence on screen.
			if enabled && !hlsEnabled && len(working) == 0 {
				blocked = "hls-disabled"
			}

			ohttp.WriteData(ctx, w, r, &struct {
				Config     recordDiagConfig   `json:"config"`
				Working    []*recordDiagEntry `json:"working"`
				Finished   []*recordDiagEntry `json:"finished"`
				Home       string             `json:"home"`
				HLSEnabled bool               `json:"hlsEnabled"`
				Blocked    string             `json:"blocked,omitempty"`
			}{conf, working, finished, recordHome, hlsEnabled, blocked})

			logger.Tf(ctx, "record query ok, all=%v hls=%v working=%v finished=%v",
				conf.All, hlsEnabled, len(working), len(finished))
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
