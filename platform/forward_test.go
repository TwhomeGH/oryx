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
