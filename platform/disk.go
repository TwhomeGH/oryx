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

// diskFS is one filesystem's capacity, so the UI can correlate "out of space" with failures.
type diskFS struct {
	// The role of this filesystem in Oryx.
	Name string `json:"name"`
	// The mount path we probed.
	Path string `json:"path"`
	// Capacity in bytes.
	TotalBytes uint64  `json:"totalBytes"`
	FreeBytes  uint64  `json:"freeBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	Percent    float64 `json:"percent"`
}

// statfs reports the capacity of the filesystem backing dir.
func statfs(dir string) (r0 diskFS, err error) {
	r0.Path = dir

	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return r0, errors.Wrapf(err, "statfs %v", dir)
	}

	bsize := uint64(st.Bsize)
	r0.TotalBytes = st.Blocks * bsize
	r0.FreeBytes = st.Bavail * bsize
	r0.UsedBytes = r0.TotalBytes - r0.FreeBytes
	if r0.TotalBytes > 0 {
		r0.Percent = float64(r0.UsedBytes) / float64(r0.TotalBytes) * 100
	}
	return
}

// handleMgmtDiskQuery reports capacity for the filesystems Oryx writes to. The data volume
// (/data, persistent: record/uploads) and the container root (/, system: HLS segments, logs) may
// live on different filesystems — e.g. a host bind mount vs the Docker/WSL virtual disk — so we
// report both instead of only the data dir.
func handleMgmtDiskQuery(ctx context.Context, handler *http.ServeMux) {
	ep := "/terraform/v1/mgmt/disk/query"
	logger.Tf(ctx, "Handle %v", ep)
	handler.Handle(ep, middlewareAuthTokenInBody(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := func() error {
			filesystems := []diskFS{}

			if _, e := os.Stat("/data"); e == nil {
				if fs, err := statfs("/data"); err != nil {
					return err
				} else {
					fs.Name = "data"
					filesystems = append(filesystems, fs)
				}
			}
			if fs, err := statfs("/"); err == nil {
				fs.Name = "system"
				filesystems = append(filesystems, fs)
			}

			ohttp.WriteData(ctx, w, r, &struct {
				Filesystems []diskFS `json:"filesystems"`
			}{Filesystems: filesystems})

			logger.Tf(ctx, "disk query ok, %v filesystems", len(filesystems))
			return nil
		}(); err != nil {
			ohttp.WriteError(ctx, w, r, err)
		}
	})))
}
