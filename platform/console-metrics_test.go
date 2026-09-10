// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ohttp "github.com/ossrs/go-oryx-lib/http"
)

// Exercise the shared JSON writer through the exact sink reported by CodeQL.
func TestConsoleMetricsJSONPIgnored(t *testing.T) {
	for _, callback := range []string{
		"alert(1)//", "cb;alert(1)//", "<script>alert(1)</script>",
		"cb\nalert(1)", "cb\n", "cb[alert(1)]", ".cb", "cb.", "cb..next",
	} {
		t.Run(callback, func(t *testing.T) {
			store := newConsoleMetricsStore(10)
			handler := store.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ohttp.WriteData(context.Background(), w, r, "ok")
			}))
			req := httptest.NewRequest(http.MethodGet, "/test?callback="+url.QueryEscape(callback), nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusOK || !json.Valid(res.Body.Bytes()) || strings.Contains(res.Body.String(), callback) {
				t.Fatalf("unsafe callback response: status=%d body=%q", res.Code, res.Body.String())
			}
			if res.Header().Get("Content-Type") != "application/json; charset=utf-8" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("unsafe error headers: %v", res.Header())
			}
		})
	}
}

func TestConsoleMetricsJSONOnly(t *testing.T) {
	for _, callback := range []string{"", "callback", "app.handlers.done", "_$cb123"} {
		t.Run(callback, func(t *testing.T) {
			handler := newConsoleMetricsStore(10).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ohttp.WriteData(context.Background(), w, r, "<script>alert(1)</script>")
			}))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/test?callback="+url.QueryEscape(callback), nil))
			if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "<script>") {
				t.Fatalf("unexpected response: %d %q", res.Code, res.Body.String())
			}
			var body struct {
				Code int    `json:"code"`
				Data string `json:"data"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatalf("response must be JSON, never JSONP: %v", err)
			}
			if body.Code != 0 || body.Data != "<script>alert(1)</script>" {
				t.Fatalf("response data changed: %+v", body)
			}
			if res.Header().Get("Content-Type") != "application/json; charset=utf-8" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("unsafe response headers: %v", res.Header())
			}
		})
	}
}

func TestConsoleMetricsJSONError(t *testing.T) {
	handler := newConsoleMetricsStore(10).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ohttp.WriteCplxError(context.Background(), w, r, ohttp.SystemError(123), r.URL.Query().Get("message"))
	}))
	res := httptest.NewRecorder()
	payload := "<script>alert(1)</script>"
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/test?callback=app.done&message="+url.QueryEscape(payload), nil))
	var body struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response must remain JSON: %v", err)
	}
	if res.Code != http.StatusOK || body.Code != 123 || body.Data != payload || strings.Contains(res.Body.String(), "<script>") {
		t.Fatalf("unexpected error response: %d %q", res.Code, res.Body.String())
	}
	if res.Header().Get("Content-Type") != "application/json; charset=utf-8" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe error headers: %v", res.Header())
	}
}

func TestNormalizeConsoleRoute(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/terraform/v1/mgmt/streams/fps", "GET /terraform/v1/mgmt/streams/fps"},
		{"POST", "/terraform/v1/mgmt/streams/kickoff", "POST /terraform/v1/mgmt/streams/kickoff"},
		{"GET", "/terraform/v1/hooks/srs/verify", "GET /terraform/v1/hooks/srs/verify"},
		{"GET", "/players/players.html", "GET /players/*"},
		{"GET", "/tools/player.html", "GET /tools/*"},
		{"GET", "/api/v1/streams", "GET /api/v1/streams"},
		{"GET", "/api/v1/clients/882ko15q", "GET /api/v1/clients/{id}"},
		{"DELETE", "/api/v1/clients/123", "DELETE /api/v1/clients/{id}"},
		{"GET", "/rtc/v1/whep/", "GET /rtc/v1/whep"},
		// Preflight, self-polling and media streams are not recorded.
		{"OPTIONS", "/terraform/v1/mgmt/envs", ""},
		{"GET", consoleMetricsPath, ""},
		{"GET", "/live/livestream.flv", ""},
		{"GET", "/live/livestream.m3u8", ""},
		{"GET", "/live/livestream.ts", ""},
	}
	for _, tt := range tests {
		if got := normalizeConsoleRoute(tt.method, tt.path); got != tt.want {
			t.Errorf("normalizeConsoleRoute(%v, %v) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestConsoleMetricsSnapshot(t *testing.T) {
	store := newConsoleMetricsStore(100)
	handler := store.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/terraform/v1/mgmt/envs", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/clients/882ko15q", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/fail", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	// Media and OPTIONS must not be recorded.
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/live/livestream.flv", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	stats := store.Snapshot()
	got := make(map[string]httpRouteStat)
	for _, s := range stats {
		got[s.Method+" "+s.Route] = s
	}
	if v, ok := got["GET /terraform/v1/mgmt/envs"]; !ok || v.Count != 5 {
		t.Errorf("mgmt envs not aggregated correctly, got=%+v", stats)
	}
	if v, ok := got["POST /api/v1/clients/{id}"]; !ok || v.Count != 2 {
		t.Errorf("clients {id} family not aggregated, got=%+v", stats)
	}
	if v, ok := got["GET /fail"]; !ok || v.ErrorCount != 1 {
		t.Errorf("5xx not counted, got=%+v", stats)
	}
	for _, s := range stats {
		if s.Route == "/live/livestream.flv" || s.Route == "/players/*" {
			t.Errorf("unexpected recorded route %+v", s)
		}
	}
}

func TestBuildRedisInfoSnapshot(t *testing.T) {
	raw := "# Server\n" +
		"redis_version:5.0.14\n" +
		"uptime_in_seconds:16314\n" +
		"# Clients\n" +
		"connected_clients:3\n" +
		"blocked_clients:0\n" +
		"# Memory\n" +
		"used_memory:1326512\n" +
		"used_memory_rss:3043328\n" +
		"used_memory_peak:1613488\n" +
		"maxmemory:0\n" +
		"maxmemory_policy:noeviction\n" +
		"mem_fragmentation_ratio:2.49\n" +
		"# Stats\n" +
		"total_commands_processed:313053\n" +
		"instantaneous_ops_per_sec:24\n" +
		"total_net_input_bytes:1000\n" +
		"total_net_output_bytes:2000\n" +
		"rejected_connections:0\n" +
		"expired_keys:0\n" +
		"evicted_keys:0\n" +
		"keyspace_hits:38320\n" +
		"keyspace_misses:274644\n" +
		"# Persistence\n" +
		"rdb_last_bgsave_status:ok\n" +
		"rdb_changes_since_last_save:9\n" +
		"aof_enabled:0\n" +
		"# Replication\n" +
		"role:master\n" +
		"# Keyspace\n" +
		"db0:keys=1243,expires=50,avg_ttl=0\n"
	obj := buildRedisInfoSnapshot(raw)
	if obj.RedisVersion != "5.0.14" || obj.ServerName != "redis" || obj.ServerVersion != "5.0.14" || obj.Role != "master" || obj.UptimeSec != 16314 {
		t.Errorf("server fields wrong, %+v", obj)
	}
	if obj.UsedMemory != 1326512 || obj.MemFragmentationRatio != 2.49 || obj.MaxmemoryPolicy != "noeviction" {
		t.Errorf("memory fields wrong, %+v", obj)
	}
	if obj.TotalCommands != 313053 || obj.InstantaneousOps != 24 || obj.KeyspaceHits != 38320 || obj.KeyspaceMisses != 274644 {
		t.Errorf("stats fields wrong, %+v", obj)
	}
	if obj.RdbLastBgsaveStatus != "ok" || obj.AofEnabled != 0 {
		t.Errorf("persistence fields wrong, %+v", obj)
	}
	if obj.DB0Keys != 1243 || obj.DB0Expires != 50 {
		t.Errorf("keyspace fields wrong, %+v", obj)
	}
}

func TestBuildRedisInfoSnapshotValkey(t *testing.T) {
	obj := buildRedisInfoSnapshot("# Server\r\nserver_name:valkey\r\nredis_version:7.2.4\r\nvalkey_version:8.1.10\r\n")
	if obj.ServerName != "valkey" || obj.ServerVersion != "8.1.10" || obj.RedisVersion != "7.2.4" {
		t.Fatalf("actual Valkey version must be separate from Redis compatibility version: %+v", obj)
	}
}

// The metrics wrapper must not hide http.Flusher, or the /live/*.flv reverse proxy
// cannot stream (the response gets buffered and playback stalls).
func TestConsoleMetricsFlushPassthrough(t *testing.T) {
	handler := newConsoleMetricsStore(10).Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := w.(http.Flusher); !ok {
			t.Fatalf("metrics wrapper must expose http.Flusher")
		} else {
			f.Flush()
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/live/livestream.flv", nil))
	if !rec.Flushed {
		t.Fatalf("Flush must reach the underlying ResponseWriter through the metrics wrapper")
	}
}
