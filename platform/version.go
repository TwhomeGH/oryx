// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"strings"
)

// The version for platform.
const version = "v5.22.12"

// buildRevision is the git revision stamped into the image (Dockerfile ARG ORYX_REVISION). It is used
// as the ?v= token on player asset URLs: css/js keep a 30-day cache, and a new build changes the URL
// so browsers refetch without any manual cache busting. "dev" means an unstamped local build.
func buildRevision() string {
	rev := strings.TrimSpace(os.Getenv("ORYX_REVISION"))
	if rev == "" || rev == "dev" {
		// The image bakes a build-unique fallback so an unstamped build still busts caches.
		if b, err := os.ReadFile("/etc/oryx-revision"); err == nil {
			rev = strings.TrimSpace(string(b))
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return rev
}
