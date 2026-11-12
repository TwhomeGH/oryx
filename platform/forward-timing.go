// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	"github.com/ossrs/go-oryx-lib/logger"

	"github.com/go-redis/redis/v8"
)

// How many recent forwarding runs keep their source timing log. The log records the ORIGINAL
// PTS/DTS of the source stream, which is the information destroyed by a CFR transcode, so it is
// kept even after the run ends: it is the only way to trace what the source actually did.
const forwardTimingKeepRuns = 5

// forwardTimingMeta describes one captured run.
type forwardTimingMeta struct {
	Platform  string `json:"platform"`
	App       string `json:"app"`
	Stream    string `json:"stream"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt,omitempty"`
	Bytes     int64  `json:"bytes"`   // uncompressed payload size
	GzBytes   int64  `json:"gzBytes"` // size of the .gz file on disk
	Lines     int64  `json:"lines"`
	File      string `json:"file"`
}

// forwardTimingRun is a live capture: stop() ends it and finalizes the metadata.
type forwardTimingRun struct {
	meta   forwardTimingMeta
	dir    string
	file   *os.File
	gz     *gzip.Writer
	cmd    *exec.Cmd
	lines  int64
	bytes  int64
	closed atomic.Bool
}

func forwardTimingRoot() string {
	return path.Join(conf.Pwd, "containers/data/forward-timing")
}

// startForwardTimingCapture records the source stream's packet timestamps for one forwarding run.
// ffprobe only demuxes (no decode), so the cost is a single extra FLV consumer.
func startForwardTimingCapture(ctx context.Context, platform, app, stream string) *forwardTimingRun {
	if rdb == nil || platform == "" || app == "" || stream == "" {
		return nil
	}
	runID := time.Now().Format("20060102-150405")
	dir := path.Join(forwardTimingRoot(), platform, runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Wf(ctx, "ignore forward timing mkdir err %+v", err)
		return nil
	}

	file := path.Join(dir, "packets.csv.gz")
	f, err := os.Create(file)
	if err != nil {
		logger.Wf(ctx, "ignore forward timing create err %+v", err)
		return nil
	}

	url := fmt.Sprintf("http://localhost:%v/%v/%v.flv", forwardProgramHTTPPort, app, stream)
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "packet=stream_index,pts_time,dts_time,duration_time,flags",
		"-of", "csv=p=0", url)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		f.Close()
		logger.Wf(ctx, "ignore forward timing pipe err %+v", err)
		return nil
	}

	r := &forwardTimingRun{
		meta: forwardTimingMeta{
			Platform: platform, App: app, Stream: stream,
			StartedAt: time.Now().Format(time.RFC3339),
			File:      strings.TrimPrefix(file, conf.Pwd+"/"),
		},
		dir: dir, file: f, gz: gzip.NewWriter(f), cmd: cmd,
	}

	if err := cmd.Start(); err != nil {
		r.Close()
		logger.Wf(ctx, "ignore forward timing start err %+v", err)
		return nil
	}

	// Copy the packet stream into the gzip file, counting lines/bytes for the metadata. Flush on a
	// timer: the gzip writer holds compressed bytes in memory, so a reader of the file (or a sizing
	// check) would otherwise see only the 10-byte header for minutes.
	go func() {
		buf := make([]byte, 32*1024)
		lastFlush := time.Now()
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				atomic.AddInt64(&r.lines, int64(strings.Count(string(buf[:n]), "\n")))
				atomic.AddInt64(&r.bytes, int64(n))
				if _, werr := r.gz.Write(buf[:n]); werr != nil {
					return
				}
			}
			if time.Since(lastFlush) > 3*time.Second {
				lastFlush = time.Now()
				_ = r.gz.Flush()
			}
			if rerr != nil {
				return
			}
		}
	}()

	writeForwardTimingMeta(r)
	logger.Tf(ctx, "forward timing capture started, platform=%v stream=%v/%v dir=%v", platform, app, stream, dir)
	pruneForwardTiming(ctx, platform)
	return r
}

// Close stops the capture and writes the metadata file.
func (r *forwardTimingRun) Close() {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return
	}
	_ = r.gz.Close()
	_ = r.file.Close()
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	_ = r.cmd.Wait()

	r.meta.EndedAt = time.Now().Format(time.RFC3339)
	r.meta.Bytes = atomic.LoadInt64(&r.bytes)
	r.meta.Lines = atomic.LoadInt64(&r.lines)
	if st, err := os.Stat(path.Join(r.dir, "packets.csv.gz")); err == nil {
		r.meta.GzBytes = st.Size()
	}
	if b, err := json.MarshalIndent(r.meta, "", "  "); err == nil {
		_ = os.WriteFile(path.Join(r.dir, "meta.json"), b, 0644)
	}
	logger.Tf(context.Background(), "forward timing capture done, platform=%v lines=%v bytes=%v",
		r.meta.Platform, r.meta.Lines, r.meta.Bytes)
}

// pruneForwardTiming keeps only the newest runs for a platform.
func pruneForwardTiming(ctx context.Context, platform string) {
	root := path.Join(forwardTimingRoot(), platform)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for i, name := range names {
		if i < forwardTimingKeepRuns {
			continue
		}
		if err := os.RemoveAll(path.Join(root, name)); err == nil {
			logger.Tf(ctx, "forward timing pruned old run %v/%v", platform, name)
		}
	}
}

// queryForwardTiming lists the retained runs for a platform (newest first).
func queryForwardTiming(ctx context.Context, platform string) ([]forwardTimingMeta, error) {
	root := path.Join(forwardTimingRoot(), platform)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "read %v", root)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	metas := []forwardTimingMeta{}
	for _, name := range names {
		b, err := os.ReadFile(path.Join(root, name, "meta.json"))
		if err != nil {
			continue
		}
		var m forwardTimingMeta
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		metas = append(metas, m)
	}
	return metas, nil
}

// ensure the io import is used even if the copy loop changes.
var _ = io.Discard

// superviseTimingCapture starts a capture whenever the source is publishing and stops it when the
// source goes away. It runs for the lifetime of the task loop, independently of doForward (which
// blocks for the whole forwarding session and therefore cannot drive per-burst bookkeeping).
func (v *ForwardTask) superviseTimingCapture(ctx context.Context) {
	if rdb == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var run *forwardTimingRun
	stop := func() {
		if run != nil {
			run.Close()
			run = nil
		}
	}
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// The source is only known after the first forwarding cycle selected it, so resolve it
			// lazily instead of giving up at start.
			app, stream := v.timingSource()
			if app == "" || stream == "" {
				continue
			}
			active, err := rdb.HExists(ctx, SRS_STREAM_ACTIVE, fmt.Sprintf("%v/%v", app, stream)).Result()
			if err != nil && err != redis.Nil {
				continue
			}
			if active && run == nil {
				run = startForwardTimingCapture(ctx, v.Platform, app, stream)
			} else if !active && run != nil {
				stop()
			}
		}
	}
}

// timingSource returns the source actually bound by the forwarding loop.
func (v *ForwardTask) timingSource() (app, stream string) {
	v.lock.Lock()
	defer v.lock.Unlock()
	return v.sourceApp, v.sourceStream
}

// writeForwardTimingMeta persists the metadata; called at start and on close so a live capture is
// listed with its current size.
func writeForwardTimingMeta(r *forwardTimingRun) {
	if r == nil {
		return
	}
	m := r.meta
	m.Bytes = atomic.LoadInt64(&r.bytes)
	m.Lines = atomic.LoadInt64(&r.lines)
	if b, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(path.Join(r.dir, "meta.json"), b, 0644)
	}
}
