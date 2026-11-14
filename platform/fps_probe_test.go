package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// The probe failure must say *why* it failed; a stalled stream that hits the deadline has to be
// reported as a timeout, not as an anonymous error.
func TestFpsProbeFailureReason(t *testing.T) {
	expired, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-expired.Done()

	if got := fpsProbeFailureReason("", nil, expired); got != FpsProbeReasonTimeout {
		t.Fatalf("expired ctx: got %v, want %v", got, FpsProbeReasonTimeout)
	}

	live, cancel2 := context.WithTimeout(context.Background(), time.Minute)
	defer cancel2()
	if got := fpsProbeFailureReason("rtmp://x: Operation timed out", nil, live); got != FpsProbeReasonTimeout {
		t.Fatalf("timed out message: got %v", got)
	}
	if got := fpsProbeFailureReason("rtmp://x: Input/output error", nil, live); got != FpsProbeReasonFailed {
		t.Fatalf("io error message: got %v", got)
	}
	// ffprobe killed by our own deadline: no message, but the error says SIGKILL.
	killed := errors.New("signal: killed")
	if got := fpsProbeFailureReason("", killed, live); got != FpsProbeReasonTimeout {
		t.Fatalf("killed probe: got %v, want %v", got, FpsProbeReasonTimeout)
	}
	if got := fpsProbeFailureReason("", nil, live); got != FpsProbeReasonFailed {
		t.Fatalf("live ctx, no message: got %v", got)
	}
}

// The reason must survive the wrapping done by queryStreamFPS; losing it silently downgrades a
// timeout to a generic failure, which is exactly the "swallowed error" the UI must never show.
// Note: this uses fmt.Errorf("%w") on purpose — the vendored go-oryx-lib errors.Wrapf does not
// implement Unwrap, so wrapping a typed error with it drops the reason.
func TestFpsProbeErrorSurvivesWrap(t *testing.T) {
	base := &FpsProbeError{Reason: FpsProbeReasonTimeout, Err: errors.New("signal: killed")}
	wrapped := fmt.Errorf("probe fps live/x: %w", base)
	pe, ok := asProbeError(wrapped)
	if !ok || pe == nil {
		t.Fatalf("wrapped error lost the typed reason: %T", wrapped)
	}
	if pe.Reason != FpsProbeReasonTimeout {
		t.Fatalf("reason: got %v, want %v", pe.Reason, FpsProbeReasonTimeout)
	}
}
