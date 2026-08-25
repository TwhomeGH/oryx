//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// srsCapability is the probe result of a single SRS feature.
type srsCapability struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// The raw output of `srs -t`, contains the error reason when failed.
	Detail string `json:"detail,omitempty"`
}

// srsBinPath returns the SRS binary to verify, configurable by env SRS_BIN, so
// that a custom-built or custom-located SRS can be checked as well.
func srsBinPath() string {
	if v := os.Getenv("SRS_BIN"); v != "" {
		return v
	}
	return "/usr/local/srs/objs/srs"
}

// srsQueryVersion runs `srs -v` and extracts the semver like 7.0.157 from output.
func srsQueryVersion(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, srsBinPath(), "-v").CombinedOutput()
	if err != nil {
		return "", errors.Wrapf(err, "run %v -v, output=%v", srsBinPath(), strings.TrimSpace(string(out)))
	}

	v := strings.TrimSpace(string(out))
	if m := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(v); m != "" {
		return m, nil
	}
	return v, nil
}

// srsTestConfig feeds confContent to `srs -t -c`, which parses and validates the
// configuration without binding any port. A feature-specific directive unknown to
// the binary (e.g., srt_server missing in a custom build) makes this fail, which
// is exactly how we detect capabilities.
func srsTestConfig(ctx context.Context, confContent string) (bool, string) {
	dir, err := os.MkdirTemp("", "srs-cap-*")
	if err != nil {
		return false, "mktemp: " + err.Error()
	}
	defer os.RemoveAll(dir)

	confFile := path.Join(dir, "probe.conf")
	if err = os.WriteFile(confFile, []byte(confContent), 0644); err != nil {
		return false, "write: " + err.Error()
	}

	out, err := exec.CommandContext(ctx, srsBinPath(), "-t", "-c", confFile).CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if len(detail) > 400 {
		detail = detail[:400]
	}
	return err == nil, detail
}

// srsProbeFeatures verifies every feature that Oryx relies on.
func srsProbeFeatures(ctx context.Context) []srsCapability {
	const (
		pListenRTMP = "listen 11935;\nmax_connections 1000;\ndaemon off;\n"
		pHTTPAPI    = "http_api {\n    enabled on;\n    listen 11985;\n}\n"
		pHTTPServer = "http_server {\n    enabled on;\n    listen 18080;\n    dir ./objs/nginx/html;\n}\n"
		pRTCSrv     = "rtc_server {\n    enabled on;\n    listen 18000;\n    candidate 127.0.0.1;\n}\n"
		pSRTSrv     = "srt_server {\n    enabled on;\n    listen 11080;\n}\n"
	)

	vh := func(inner string) string {
		return "vhost __defaultVhost__ {\n" + inner + "}\n"
	}

	type probe struct {
		name   string
		config string
	}
	probes := []probe{
		{name: "rtmp", config: pListenRTMP},
		{name: "http_api", config: pListenRTMP + pHTTPAPI},
		{name: "hls", config: pListenRTMP + pHTTPServer +
			vh("    hls {\n        enabled on;\n        hls_path ./objs/nginx/html;\n    }\n")},
		{name: "webrtc", config: pListenRTMP + pRTCSrv +
			vh("    rtc {\n        enabled on;\n    }\n")},
		{name: "srt", config: pListenRTMP + pSRTSrv},
		// Native protocol-level forward, see forward-architecture.md track 2.
		{name: "forward", config: pListenRTMP + vh("    forward 127.0.0.1:19999;\n")},
		{name: "hooks", config: pListenRTMP +
			vh("    http_hooks {\n        enabled on;\n        on_publish http://127.0.0.1:10000/terraform/v1/hooks/srs/verify;\n    }\n")},
	}

	res := make([]srsCapability, 0)
	for _, p := range probes {
		ok, detail := srsTestConfig(ctx, p.config)
		logger.Tf(ctx, "srs capability probe name=%v, ok=%v, detail=%v", p.name, ok, detail)
		res = append(res, srsCapability{Name: p.name, OK: ok, Detail: detail})
	}
	return res
}

// handleSrsCapabilities exposes an API to verify whether the current SRS binary
// supports the features Oryx depends on. This is the gate before switching to a
// self-maintained/custom SRS build.
func handleSrsCapabilities(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/srs/capabilities"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			version, verr := srsQueryVersion(ctx)
			features := srsProbeFeatures(ctx)

			var versionErr string
			if verr != nil {
				versionErr = verr.Error()
			}

			ohttp.WriteData(ctx, w, r, &struct {
				Version    string          `json:"version"`
				VersionErr string          `json:"versionError,omitempty"`
				Bin        string          `json:"bin"`
				Features   []srsCapability `json:"features"`
			}{
				Version: version, VersionErr: versionErr, Bin: srsBinPath(), Features: features,
			})
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
