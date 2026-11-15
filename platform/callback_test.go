package main

import "testing"

// A callback target usually carries its token in the path, so the console must show the host and
// never the token.
func TestMaskCallbackTarget(t *testing.T) {
	got := maskCallbackTarget("https://partner.example.com/hooks/abc123secret?token=xyz")
	if got != "https://partner.example.com/***" {
		t.Fatalf("mask = %q, want scheme+host only", got)
	}
	if got := maskCallbackTarget("not a url"); got != "***" {
		t.Fatalf("unparseable target should be fully masked, got %q", got)
	}
	if got := maskCallbackTarget(""); got != "***" {
		t.Fatalf("empty target should be masked, got %q", got)
	}
}

// Recording must be a no-op without Redis so unit tests and a Redis-less boot never panic.
func TestRecordCallbackWithoutRedis(t *testing.T) {
	saved := rdb
	rdb = nil
	defer func() { rdb = saved }()

	recordCallback(nil, callbackRecord{Action: "test", Result: "ok"})
}
