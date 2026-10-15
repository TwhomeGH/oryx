package main

import (
	"testing"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
)

func TestForward_RetryDelay(t *testing.T) {
	for _, e := range []struct {
		failures int
		expect   time.Duration
	}{
		{failures: 0, expect: forwardBaseRetryDelay},
		{failures: 1, expect: forwardBaseRetryDelay},
		{failures: 2, expect: 7 * time.Second},
		{failures: 3, expect: 14 * time.Second},
		{failures: 4, expect: 28 * time.Second},
		{failures: 5, expect: 56 * time.Second},
		{failures: 6, expect: forwardMaxRetryDelay},
		{failures: 20, expect: forwardMaxRetryDelay},
	} {
		if delay := forwardRetryDelay(e.failures); delay != e.expect {
			t.Fatalf("forwardRetryDelay(%v)=%v, expect %v", e.failures, delay, e.expect)
		}
	}
}

func TestForward_ProgramStream(t *testing.T) {
	if got := forwardProgramStreamName("live", "livestream"); got != "__forward_live_livestream" {
		t.Fatalf("forwardProgramStreamName=%v", got)
	}

	for _, e := range []struct {
		app, stream string
		expect      bool
	}{
		{app: "live", stream: "__forward_live_livestream", expect: true},
		{app: "live", stream: "__forward_auto_live", expect: true},
		{app: "live", stream: "livestream", expect: false},
		{app: "live", stream: "forward_livestream", expect: false},
	} {
		if got := isForwardProgramStream(e.app, e.stream); got != e.expect {
			t.Fatalf("isForwardProgramStream(%v, %v)=%v, expect %v", e.app, e.stream, got, e.expect)
		}
	}
}

func TestForward_TaskHealthState(t *testing.T) {
	task := &ForwardTask{config: &ForwardConfigure{Enabled: true}}

	task.markWaitingInput()
	if status := task.queryStatus(); status.State != forwardStateWaiting {
		t.Fatalf("invalid waiting status %+v", status)
	}

	for i := 1; i <= forwardCircuitThreshold; i++ {
		task.markFailure(errors.Errorf("target failed %v", i))
	}

	status := task.queryStatus()
	if status.State != forwardStateDegraded {
		t.Fatalf("invalid degraded status %+v", status)
	}
	if status.ConsecutiveFailures != forwardCircuitThreshold {
		t.Fatalf("invalid failures %+v", status)
	}
	if status.RestartCount != forwardCircuitThreshold {
		t.Fatalf("invalid restart count %+v", status)
	}
	if status.LastError == "" || status.LastErrorAt == "" || status.NextRetryAt == "" {
		t.Fatalf("missing error observability %+v", status)
	}

	task.markHealthy()
	status = task.queryStatus()
	if status.State != forwardStateWaiting {
		t.Fatalf("invalid healthy status %+v", status)
	}
	if status.ConsecutiveFailures != 0 || status.LastError != "" || status.NextRetryAt != "" {
		t.Fatalf("health reset failed %+v", status)
	}
}

func TestForward_ProgressMetrics(t *testing.T) {
	// A real fanout progress line (FrameLogs normalizes "= " to "=").
	line := "frame=53731 fps=60 q=-1.0 size=444182KiB time=00:18:33.45 bitrate=3268.0kbits/s speed=1.24x"

	progress, ok := parseFFmpegProgress(line)
	if !ok {
		t.Fatalf("parse failed for %v", line)
	}
	if progress.frame != 53731 || progress.fps != 60 || progress.speed != 1.24 {
		t.Fatalf("invalid progress %+v", progress)
	}
	if progress.mediaTime != "00:18:33.45" || progress.mediaSec != 1113.45 {
		t.Fatalf("invalid media time %+v", progress)
	}
	if progress.sizeBytes != 444182*1024 || progress.bitrateKbps != 3268.0 {
		t.Fatalf("invalid size/bitrate %+v", progress)
	}

	// The raw line may still carry spaces after '='.
	spaced := "frame= 180 fps= 90 q=-1.0 size=   1024kB time=00:00:03.00 bitrate=2796.2kbits/s speed= 1.5x"
	if p, ok := parseFFmpegProgress(spaced); !ok || p.frame != 180 || p.speed != 1.5 || p.sizeBytes != 1024*1000 {
		t.Fatalf("invalid spaced progress %+v, ok=%v", p, ok)
	}

	// Unavailable values must not look like real zero.
	for _, bad := range []string{"", "fps=N/A time=N/A speed=N/A", "Exiting normally, received signal 2."} {
		if _, ok := parseFFmpegProgress(bad); ok {
			t.Fatalf("expected unparsable: %q", bad)
		}
	}

	// updateFrame populates the status metrics, and an unparsable line keeps the last value.
	task := &ForwardTask{config: &ForwardConfigure{Enabled: true}}
	now := time.Now()
	task.starttime = &now
	task.updateFrame(line)

	status := task.queryStatus()
	if status.Metrics == nil {
		t.Fatalf("missing metrics")
	}
	if status.Metrics.OutputFPS != 60 || status.Metrics.Speed != 1.24 || status.Metrics.AvgFPS == 0 {
		t.Fatalf("invalid metrics %+v", status.Metrics)
	}

	task.updateFrame("Exiting normally, received signal 2.")
	if m := task.queryStatus().Metrics; m == nil || m.Speed != 1.24 {
		t.Fatalf("metrics must survive an unparsable line: %+v", m)
	}
}

func TestForward_Diagnostics(t *testing.T) {
	task := &ForwardTask{config: &ForwardConfigure{Enabled: true}}
	task.markRunning(1, "live/src", "live/__forward_live_src", "rtmp://localhost/live/out")
	task.markFailure(errors.Errorf("boom"))
	task.markHealthy()

	status := task.queryStatus()
	var names []string
	for _, e := range status.Events {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != "start" || names[1] != "failure" || names[2] != "recovered" {
		t.Fatalf("invalid events %v", names)
	}

	// Recent logs keep only diagnostic lines, bounded and timestamped.
	task.addLog("ffmpeg version N-127259 ...") // banner noise is dropped
	for i := 0; i < forwardDiagRingSize+10; i++ {
		task.addLog("HTTP error 502 Bad Gateway")
	}
	if len(task.queryStatus().Logs) != forwardDiagRingSize {
		t.Fatalf("logs not bounded: %v", len(task.queryStatus().Logs))
	}
}
