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
	// Keep the whole token: a local build appends "-dirty-<timestamp>" so each rebuild changes the
	// asset URLs. Capping at 64 still truncates nothing in practice (40-char sha + suffix), while a
	// shorter cap would silently cut the suffix off and make every dirty build look identical.
	if len(rev) > 64 {
		rev = rev[:64]
	}
	return rev
}
