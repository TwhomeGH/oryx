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
	Available  bool         `json:"available"`
	Name       string       `json:"name,omitempty"`
	UtilPct    int          `json:"utilPct"`
	MemUsedMB  int          `json:"memUsedMB"`
	MemTotalMB int          `json:"memTotalMB"`
	Processes  []gpuProcess `json:"processes"`
}

// queryGPU reads the first GPU via nvidia-smi. An absent GPU returns Available=false, not an
// error, because a CPU-only deployment is normal.
func queryGPU(ctx context.Context) gpuInfo {
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader").Output()
	if err != nil {
		return gpuInfo{}
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	parts := strings.Split(line, ",")
	if len(parts) != 4 {
		return gpuInfo{}
	}
	info := gpuInfo{Available: true, Name: strings.TrimSpace(parts[0])}
	info.UtilPct, _ = strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(parts[1], "%")))
	info.MemUsedMB, _ = strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(parts[2], "MiB")))
	info.MemTotalMB, _ = strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(parts[3], "MiB")))

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
