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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// Detect which encoders the installed ffmpeg actually supports, including
// hardware encoders such as h264_nvenc (NVIDIA), h264_qsv (Intel) and
// h264_vaapi (AMD/Intel). Listing an encoder does not guarantee it works,
// so each hardware encoder is also probed with a tiny real encoding task.
// The result is cached in memory and refreshed periodically or on demand.
type FFmpegEncoderCapability struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Hardware devices relevant for hardware encoding, probed from the
// filesystem so the UI can explain WHY an encoder fails.
type FFmpegDeviceCapability struct {
	Name string `json:"name"` // e.g. "NVIDIA", "DRM/VAAPI"
	Path string `json:"path"`
	OK   bool   `json:"ok"`
}

type FFmpegCapabilities struct {
	Update   time.Time                   `json:"update"`
	Probing  bool                        `json:"probing"`
	Version  string                      `json:"version,omitempty"`
	Encoders []FFmpegEncoderCapability   `json:"encoders"`
	Devices  []FFmpegDeviceCapability    `json:"devices,omitempty"`
}

var (
	ffmpegCapabilities     *FFmpegCapabilities
	ffmpegCapabilitiesLock sync.Mutex
)

const ffmpegCapabilityTTL = 10 * time.Minute

func listFFmpegEncoders(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return nil, errors.Wrapf(err, "ffmpeg encoders %v", string(out))
	}

	var encoders []string
	for _, line := range strings.Split(string(out), "\n") {
		// Each row looks like: " V....D libx264              libx264 H.264 ..."
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "V") {
			continue
		}
		encoders = append(encoders, fields[1])
	}
	return encoders, nil
}

// probeFFmpegEncoder runs a tiny real encoding task to verify the encoder
// works on this machine, because a listed encoder may still fail due to
// missing GPU, driver or device permissions.
func probeFFmpegEncoder(ctx context.Context, name string) (bool, string) {
	args := []string{"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=0.05:size=320x240:rate=10"}

	switch {
	case strings.HasSuffix(name, "_vaapi"):
		// VAAPI requires an explicit drm device and hw frames upload.
		args = append(args,
			"-vaapi_device", "/dev/dri/renderD128",
			"-vf", "format=nv12,hwupload",
		)
	case strings.HasSuffix(name, "_qsv"):
		args = append(args, "-init_hw_device", "qsv=hw:any", "-filter_hw_device", "hw")
	}

	args = append(args, "-frames:v", "3", "-c:v", name, "-f", "null", "-")

	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	out, err := exec.CommandContext(cctx, "ffmpeg", args...).CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if len(detail) > 200 {
		detail = detail[len(detail)-200:]
	}
	return err == nil, detail
}

func probeFFmpegDevices(_ context.Context) []FFmpegDeviceCapability {
	var devices []FFmpegDeviceCapability

	add := func(name, pattern string) {
		paths, _ := filepath.Glob(pattern)
		for _, p := range paths {
			devices = append(devices, FFmpegDeviceCapability{Name: name, Path: p, OK: true})
		}
	}
	add("NVIDIA", "/dev/nvidia*")
	add("DRM/VAAPI", "/dev/dri/renderD*")

	if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
		devices = append(devices, FFmpegDeviceCapability{Name: "NVIDIA-driver", Path: "/proc/driver/nvidia/version", OK: true})
	}
	return devices
}

func refreshFFmpegCapabilities(ctx context.Context) *FFmpegCapabilities {
	ffmpegCapabilitiesLock.Lock()
	defer ffmpegCapabilitiesLock.Unlock()

	if ffmpegCapabilities != nil && time.Since(ffmpegCapabilities.Update) < ffmpegCapabilityTTL {
		return ffmpegCapabilities
	}

	logger.Tf(ctx, "ffmpeg capabilities probing start")

	caps := &FFmpegCapabilities{Update: time.Now(), Probing: true}

	if vout, verr := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-version").CombinedOutput(); verr == nil {
		if lines := strings.Split(string(vout), "\n"); len(lines) > 0 {
			caps.Version = strings.TrimSpace(lines[0])
		}
	}

	interested := map[string]bool{
		"libx264": true, "libx265": true,
		"h264_nvenc": true, "hevc_nvenc": true,
		"h264_qsv": true, "hevc_qsv": true,
		"h264_vaapi": true, "hevc_vaapi": true,
		"h264_amf": true,
	}

	listed, err := listFFmpegEncoders(ctx)
	if err != nil {
		logger.Wf(ctx, "ffmpeg capabilities ignore err %+v", err)
		caps.Encoders = append(caps.Encoders, FFmpegEncoderCapability{Name: "libx264", OK: false, Detail: "no ffmpeg"})
		ffmpegCapabilities = caps
		return caps
	}

	for _, name := range listed {
		if !interested[name] {
			continue
		}

		capability := FFmpegEncoderCapability{Name: name}
		switch name {
		case "libx264", "libx265":
			// Software encoders always work if listed.
			capability.OK = true
		default:
			capability.OK, capability.Detail = probeFFmpegEncoder(ctx, name)
		}
		caps.Encoders = append(caps.Encoders, capability)
	}

	caps.Probing = false
	caps.Devices = probeFFmpegDevices(ctx)
	ffmpegCapabilities = caps

	for _, c := range caps.Encoders {
		logger.Tf(ctx, "ffmpeg capabilities encoder=%v, ok=%v, detail=%v", c.Name, c.OK, c.Detail)
	}
	return caps
}

func handleMgmtFFmpegCapabilities(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/ffmpeg/capabilities"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			var req struct {
				Refresh bool `json:"refresh"`
			}
			if err := ParseBody(ctx, r.Body, &req); err != nil {
				return errors.Wrapf(err, "parse body")
			}

			if req.Refresh {
				ffmpegCapabilitiesLock.Lock()
				ffmpegCapabilities = nil
				ffmpegCapabilitiesLock.Unlock()
			}

			caps := refreshFFmpegCapabilities(ctx)
			ohttp.WriteData(ctx, w, r, caps)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
