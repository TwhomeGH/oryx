// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"net/http"
	"os"
	"syscall"

	"github.com/ossrs/go-oryx-lib/errors"
	ohttp "github.com/ossrs/go-oryx-lib/http"
	"github.com/ossrs/go-oryx-lib/logger"
)

// diskUsage reports the capacity of the filesystem backing the platform data dir, so the UI can
// correlate "out of space" with failures (HLS/DVR/record all write the same disk).
func diskUsage() (total, free, used uint64, percent float64, err error) {
	dir := "/data"
	if _, e := os.Stat(dir); e != nil {
		dir = "."
	}

	var st syscall.Statfs_t
	if e := syscall.Statfs(dir, &st); e != nil {
		return 0, 0, 0, 0, errors.Wrapf(e, "statfs %v", dir)
	}

	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free = st.Bavail * bsize
	used = total - free
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}
	return
}

func handleMgmtDiskQuery(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/disk/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			total, free, used, percent, err := diskUsage()
			if err != nil {
				return errors.Wrapf(err, "disk usage")
			}

			ohttp.WriteData(ctx, w, r, &struct {
				TotalBytes uint64  `json:"totalBytes"`
				FreeBytes  uint64  `json:"freeBytes"`
				UsedBytes  uint64  `json:"usedBytes"`
				Percent    float64 `json:"percent"`
			}{TotalBytes: total, FreeBytes: free, UsedBytes: used, Percent: percent})

			logger.Tf(ctx, "disk query ok, free=%v/%v (%.1f%%)", free, total, percent)
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
