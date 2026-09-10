// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// The console (routers-console Redis/API tab) monitors two platform-level concerns
// that SRS itself cannot report: the load of the platform Redis (INFO), and the
// latency/errors of the platform HTTP endpoints (this file's middleware). Both are
// served through authenticated mgmt endpoints the console polls every few seconds.

// consoleMetricsPath is the endpoint that reports the HTTP latency metrics itself.
// Requests to it are not recorded, so the console polling does not inflate numbers.
const consoleMetricsPath = "/terraform/v1/mgmt/http/metrics"

// consoleMetricsWindow keeps each request sample for this long.
const consoleMetricsWindow = 60 * time.Second

// consoleMetricsSlowMs: an endpoint taking longer than this is flagged as slow.
const consoleMetricsSlowMs = int64(1000)

// consoleMetricsSample is one measured request.
type consoleMetricsSample struct {
	at  int64 // Unix milliseconds
	us  int64 // handler duration in microseconds
	err bool  // response status >= 500
}

// consoleMetricsStore keeps a bounded rolling window of request latencies, grouped
// by a normalized route, for the "endpoint latency" analysis in the console.
type consoleMetricsStore struct {
	mu       sync.Mutex
	routes   map[string][]consoleMetricsSample
	maxTotal int
}

func newConsoleMetricsStore(maxTotal int) *consoleMetricsStore {
	return &consoleMetricsStore{routes: make(map[string][]consoleMetricsSample), maxTotal: maxTotal}
}

// consoleMetrics is the shared store for the platform HTTP server.
var consoleMetrics = newConsoleMetricsStore(100000)

// statusRecorder captures the response status so the middleware can count 5xx.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (v *statusRecorder) WriteHeader(code int) {
	if v.status == 0 {
		v.status = code
	}
	v.ResponseWriter.WriteHeader(code)
}

func (v *statusRecorder) Write(b []byte) (int, error) {
	if v.status == 0 {
		v.status = http.StatusOK
	}
	return v.ResponseWriter.Write(b)
}

// Wrap returns a handler that measures the request latency and records it, then
// delegates to next. It is applied around the whole platform HTTP handler, so every
// mgmt API, SRS proxy and static/media route is covered.
func (v *consoleMetricsStore) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		route := normalizeConsoleRoute(r.Method, r.URL.Path)
		if route == "" {
			return
		}
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		v.record(route, start, status)
	})
}

func (v *consoleMetricsStore) record(route string, start time.Time, status int) {
	now := time.Now()
	sample := consoleMetricsSample{at: now.UnixMilli(), us: now.Sub(start).Microseconds(), err: status >= 500}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Prune the samples older than the window before appending, so a route keeps
	// roughly one window of history.
	cutoff := now.Add(-consoleMetricsWindow).UnixMilli()
	old := v.routes[route]
	first := 0
	for first < len(old) && old[first].at < cutoff {
		first++
	}
	if first > 0 {
		old = append([]consoleMetricsSample(nil), old[first:]...)
	}
	old = append(old, sample)
	v.routes[route] = old

	// Bound the total samples across all routes, dropping the oldest whole route
	// when over budget, so a busy media path cannot grow the memory unbounded.
	total := 0
	for _, samples := range v.routes {
		total += len(samples)
	}
	if total > v.maxTotal {
		var oldestRoute string
		var oldestAt int64 = 1 << 62
		for name, samples := range v.routes {
			if len(samples) > 0 && samples[0].at < oldestAt {
				oldestAt = samples[0].at
				oldestRoute = name
			}
		}
		if oldestRoute != "" {
			delete(v.routes, oldestRoute)
		}
	}
}

// httpRouteStat is the aggregated latency statistics of one route in the window.
type httpRouteStat struct {
	Method     string  `json:"method"`
	Route      string  `json:"route"`
	Count      int64   `json:"count"`
	RatePerSec float64 `json:"rate_per_sec"`
	AvgMs      float64 `json:"avg_ms"`
	MaxMs      float64 `json:"max_ms"`
	P95Ms      float64 `json:"p95_ms"`
	ErrorCount int64   `json:"error_count"`
	SlowCount  int64   `json:"slow_count"`
}

// Snapshot aggregates the samples in the window, sorted by max latency first.
func (v *consoleMetricsStore) Snapshot() []httpRouteStat {
	cutoff := time.Now().Add(-consoleMetricsWindow).UnixMilli()

	v.mu.Lock()
	defer v.mu.Unlock()

	stats := make([]httpRouteStat, 0, len(v.routes))
	for route, samples := range v.routes {
		// Drop the expired tail of the window lazily.
		first := 0
		for first < len(samples) && samples[first].at < cutoff {
			first++
		}
		if first > 0 {
			samples = append([]consoleMetricsSample(nil), samples[first:]...)
			v.routes[route] = samples
		}
		if len(samples) == 0 {
			continue
		}

		var totalUs, maxUs int64
		var errorsN, slowN int64
		durations := make([]int64, 0, len(samples))
		for _, s := range samples {
			totalUs += s.us
			if s.us > maxUs {
				maxUs = s.us
			}
			if s.err {
				errorsN++
			}
			if s.us >= consoleMetricsSlowMs*1000 {
				slowN++
			}
			durations = append(durations, s.us)
		}

		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95 := int64(0)
		if n := len(durations); n > 0 {
			p95 = durations[(n*95+99)/100-1]
			if p95 < 0 {
				p95 = durations[n-1]
			}
		}

		parts := strings.SplitN(route, " ", 2)
		method, normalized := parts[0], route
		if len(parts) == 2 {
			normalized = parts[1]
		}
		stats = append(stats, httpRouteStat{
			Method:     method,
			Route:      normalized,
			Count:      int64(len(samples)),
			RatePerSec: float64(len(samples)) / consoleMetricsWindow.Seconds(),
			AvgMs:      float64(totalUs) / 1000.0 / float64(len(samples)),
			MaxMs:      float64(maxUs) / 1000.0,
			P95Ms:      float64(p95) / 1000.0,
			ErrorCount: errorsN,
			SlowCount:  slowN,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].MaxMs != stats[j].MaxMs {
			return stats[i].MaxMs > stats[j].MaxMs
		}
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		return stats[i].Route < stats[j].Route
	})
	return stats
}

// normalizeConsoleRoute maps a request to a stable "METHOD /route" key, collapsing
// per-resource ids and static/media paths so the console shows route families.
// It returns "" for requests that should not be recorded (OPTIONS preflight and the
// metrics endpoint's own polling).
func normalizeConsoleRoute(method, path string) string {
	if method == http.MethodOptions || path == consoleMetricsPath {
		return ""
	}

	// Static players/tools pages are served from disk with a long cache header;
	// keep them as one family each rather than one entry per file.
	if strings.HasPrefix(path, "/players/") {
		return method + " /players/*"
	}
	if strings.HasPrefix(path, "/tools/") {
		return method + " /tools/*"
	}

	// Media streams proxied to SRS (.flv/.m3u8/.ts/.aac/.mp3) are long-lived or
	// bulk transfers, so their "latency" is meaningless and would dominate the
	// slow-endpoint list. Skip them entirely; only API-like routes are reported.
	if strings.HasSuffix(path, ".flv") || strings.HasSuffix(path, ".m3u8") ||
		strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".aac") ||
		strings.HasSuffix(path, ".mp3") {
		return ""
	}

	// mgmt and hook paths are fixed routes registered by the platform; keep them
	// exact so each mgmt API is reported separately.
	if strings.HasPrefix(path, "/terraform/") {
		return method + " " + path
	}

	// Proxied SRS APIs (/api/v1/..., /rtc/...) carry resource ids in the tail;
	// keep the family and collapse the rest to {id}.
	parts := make([]string, 0, 4)
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	keep := parts[0]
	for i := 1; i < len(parts) && i < 3; i++ {
		keep += "/" + parts[i]
	}
	if len(parts) > 3 {
		keep += "/{id}"
	}
	return method + " /" + keep
}

func handleMgmtHttpMetrics(ctx context.Context, handler *http.ServeMux) {
	ep := consoleMetricsPath
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			ohttp.WriteData(ctx, w, r, &struct {
				WindowSec int             `json:"window_sec"`
				Routes    []httpRouteStat `json:"routes"`
			}{
				WindowSec: int(consoleMetricsWindow / time.Second),
				Routes:    consoleMetrics.Snapshot(),
			})
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}

// ── Redis INFO ──

// redisInfoSnapshot is the parsed subset of Redis INFO the console displays. All
// counters are absolute, so the client computes rates from the deltas between polls.
type redisInfoSnapshot struct {
	ServerName    string `json:"server_name"`
	ServerVersion string `json:"server_version"`
	RedisVersion  string `json:"redis_version"`
	Role          string `json:"role"`
	UptimeSec     int64  `json:"uptime_sec"`

	ConnectedClients int64 `json:"connected_clients"`
	BlockedClients   int64 `json:"blocked_clients"`
	RejectedConns    int64 `json:"rejected_connections"`

	UsedMemory            int64   `json:"used_memory"`
	UsedMemoryRSS         int64   `json:"used_memory_rss"`
	UsedMemoryPeak        int64   `json:"used_memory_peak"`
	MaxMemory             int64   `json:"maxmemory"`
	MaxmemoryPolicy       string  `json:"maxmemory_policy"`
	MemFragmentationRatio float64 `json:"mem_fragmentation_ratio"`

	TotalCommands    int64 `json:"total_commands_processed"`
	InstantaneousOps int64 `json:"instantaneous_ops_per_sec"`
	TotalNetInput    int64 `json:"total_net_input_bytes"`
	TotalNetOutput   int64 `json:"total_net_output_bytes"`
	ExpiredKeys      int64 `json:"expired_keys"`
	EvictedKeys      int64 `json:"evicted_keys"`
	KeyspaceHits     int64 `json:"keyspace_hits"`
	KeyspaceMisses   int64 `json:"keyspace_misses"`

	RdbLastBgsaveStatus string `json:"rdb_last_bgsave_status"`
	RdbChangesSinceSave int64  `json:"rdb_changes_since_last_save"`
	AofEnabled          int64  `json:"aof_enabled"`

	DB0Keys    int64 `json:"db0_keys"`
	DB0Expires int64 `json:"db0_expires"`
}

// parseRedisInfo turns the raw INFO output into section/key/value maps.
func parseRedisInfo(raw string) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	var current string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "#") {
			current = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#")))
			if _, ok := sections[current]; !ok {
				sections[current] = make(map[string]string)
			}
			continue
		}
		if current == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			sections[current][key] = value
		}
	}
	return sections
}

func redisInfoInt(sections map[string]map[string]string, section, key string) int64 {
	value, _ := sections[section][key]
	if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		return n
	}
	return 0
}

func redisInfoFloat(sections map[string]map[string]string, section, key string) float64 {
	value, _ := sections[section][key]
	if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
		return n
	}
	return 0
}

func redisInfoString(sections map[string]map[string]string, section, key string) string {
	value, _ := sections[section][key]
	return strings.TrimSpace(value)
}

func buildRedisInfoSnapshot(raw string) *redisInfoSnapshot {
	sections := parseRedisInfo(raw)
	serverName, serverVersion := "redis", redisInfoString(sections, "server", "redis_version")
	if version := redisInfoString(sections, "server", "valkey_version"); version != "" {
		serverName, serverVersion = "valkey", version
	}
	obj := &redisInfoSnapshot{
		ServerName:            serverName,
		ServerVersion:         serverVersion,
		RedisVersion:          redisInfoString(sections, "server", "redis_version"),
		Role:                  redisInfoString(sections, "replication", "role"),
		UptimeSec:             redisInfoInt(sections, "server", "uptime_in_seconds"),
		ConnectedClients:      redisInfoInt(sections, "clients", "connected_clients"),
		BlockedClients:        redisInfoInt(sections, "clients", "blocked_clients"),
		RejectedConns:         redisInfoInt(sections, "stats", "rejected_connections"),
		UsedMemory:            redisInfoInt(sections, "memory", "used_memory"),
		UsedMemoryRSS:         redisInfoInt(sections, "memory", "used_memory_rss"),
		UsedMemoryPeak:        redisInfoInt(sections, "memory", "used_memory_peak"),
		MaxMemory:             redisInfoInt(sections, "memory", "maxmemory"),
		MaxmemoryPolicy:       redisInfoString(sections, "memory", "maxmemory_policy"),
		MemFragmentationRatio: redisInfoFloat(sections, "memory", "mem_fragmentation_ratio"),
		TotalCommands:         redisInfoInt(sections, "stats", "total_commands_processed"),
		InstantaneousOps:      redisInfoInt(sections, "stats", "instantaneous_ops_per_sec"),
		TotalNetInput:         redisInfoInt(sections, "stats", "total_net_input_bytes"),
		TotalNetOutput:        redisInfoInt(sections, "stats", "total_net_output_bytes"),
		ExpiredKeys:           redisInfoInt(sections, "stats", "expired_keys"),
		EvictedKeys:           redisInfoInt(sections, "stats", "evicted_keys"),
		KeyspaceHits:          redisInfoInt(sections, "stats", "keyspace_hits"),
		KeyspaceMisses:        redisInfoInt(sections, "stats", "keyspace_misses"),
		RdbLastBgsaveStatus:   redisInfoString(sections, "persistence", "rdb_last_bgsave_status"),
		RdbChangesSinceSave:   redisInfoInt(sections, "persistence", "rdb_changes_since_last_save"),
		AofEnabled:            redisInfoInt(sections, "persistence", "aof_enabled"),
	}

	// Keyspace, e.g. db0:keys=1243,expires=50,avg_ttl=0
	if db, ok := sections["keyspace"]["db0"]; ok {
		for _, part := range strings.Split(db, ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch key {
			case "keys":
				obj.DB0Keys, _ = strconv.ParseInt(value, 10, 64)
			case "expires":
				obj.DB0Expires, _ = strconv.ParseInt(value, 10, 64)
			}
		}
	}
	return obj
}

func handleMgmtRedisInfo(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/redis/info"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			raw, err := rdb.Info(ctx).Result()
			if err != nil {
				return errors.Wrapf(err, "query redis info")
			}
			obj := buildRedisInfoSnapshot(raw)
			ohttp.WriteData(ctx, w, r, obj)
			logger.Tf(ctx, "datastore info ok, server=%v, version=%v, mem=%v, ops=%v, hits=%v, misses=%v",
				obj.ServerName, obj.ServerVersion, obj.UsedMemory, obj.InstantaneousOps, obj.KeyspaceHits, obj.KeyspaceMisses)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
