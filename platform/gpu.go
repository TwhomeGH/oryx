// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// gpuInfo is the host GPU state, so failures can be correlated with encoder saturation.
// gpuProcess is one process the GPU reports as a compute client.
type gpuProcess struct {
	PID    int    `json:"pid"`
	Name   string `json:"name"`
	UsedMB int    `json:"usedMB"`
}

type gpuInfo struct {
	Available   bool    `json:"available"`
	Name        string  `json:"name,omitempty"`
	UtilPct     int     `json:"utilPct"`
	MemUtilPct  int     `json:"memUtilPct"`
	EncUtilPct  int     `json:"encUtilPct"`
	DecUtilPct  int     `json:"decUtilPct"`
	MemUsedMB   int     `json:"memUsedMB"`
	MemTotalMB  int     `json:"memTotalMB"`
	TempC       int     `json:"tempC"`
	PowerW      float64 `json:"powerW"`
	PowerLimitW float64 `json:"powerLimitW"`
	ClockMHz    int     `json:"clockMhz"`
	// NVENC/NVDEC session counts show whether Oryx itself is saturating the encoder.
	EncSessions int          `json:"encSessions"`
	DecSessions int          `json:"decSessions"`
	Processes   []gpuProcess `json:"processes"`
}

// queryGPU reads the first GPU via nvidia-smi. An absent GPU returns Available=false, not an
// error, because a CPU-only deployment is normal.
func queryGPU(ctx context.Context) gpuInfo {
	// Query the full set; a driver that does not support a field simply reports N/A, which the
	// per-field parsers tolerate.
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,utilization.gpu,utilization.memory,utilization.encoder,utilization.decoder,"+
			"memory.used,memory.total,temperature.gpu,power.draw,power.limit,clocks.current.sm",
		"--format=csv,noheader").Output()
	if err != nil {
		return gpuInfo{}
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	parts := strings.Split(line, ",")
	if len(parts) < 11 {
		return gpuInfo{}
	}
	num := func(i int, suffix string) int {
		v, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[i]), suffix)))
		return v
	}
	flt := func(i int, suffix string) float64 {
		v, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[i]), suffix)), 64)
		return v
	}
	info := gpuInfo{Available: true, Name: strings.TrimSpace(parts[0])}
	info.UtilPct = num(1, "%")
	info.MemUtilPct = num(2, "%")
	info.EncUtilPct = num(3, "%")
	info.DecUtilPct = num(4, "%")
	info.MemUsedMB = num(5, "MiB")
	info.MemTotalMB = num(6, "MiB")
	info.TempC = num(7, "")
	info.PowerW = flt(8, "W")
	info.PowerLimitW = flt(9, "W")
	info.ClockMHz = num(10, "MHz")
	if out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=encoder.stats.sessionCount,decoder.stats.sessionCount", "--format=csv,noheader").Output(); err == nil {
		fields := strings.Split(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), ",")
		if len(fields) == 2 {
			info.EncSessions, _ = strconv.Atoi(strings.TrimSpace(fields[0]))
			info.DecSessions, _ = strconv.Atoi(strings.TrimSpace(fields[1]))
		}
	}

	if out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-compute-apps=pid,process_name,used_memory", "--format=csv,noheader").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Split(line, ",")
			if len(fields) != 3 {
				continue
			}
			pid, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
			used, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(fields[2], "MiB")))
			info.Processes = append(info.Processes, gpuProcess{
				PID: pid, Name: strings.TrimSpace(fields[1]), UsedMB: used,
			})
		}
	}
	return info
}

// handleMgmtGPUQuery reports GPU utilization (empty when no GPU is exposed to the container).
func handleMgmtGPUQuery(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/gpu/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			ohttp.WriteData(ctx, w, r, queryGPU(ctx))
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
