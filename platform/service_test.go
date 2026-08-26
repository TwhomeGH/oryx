//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Register all HTTP routes on a fresh mux. A duplicate route registration
// panics inside http.ServeMux, so this test catches copy-paste mistakes
// (such as registering the same endpoint twice) before deployment.
func TestService_MuxRegistrations_NoDuplicate(t *testing.T) {
	ctx := context.Background()
	handler := http.NewServeMux()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("register routes panic: %v", r)
		}
	}()

	if err := handleHTTPService(ctx, handler); err != nil {
		t.Fatalf("handleHTTPService err %+v", err)
	}

	// Sanity check: the mux must contain our key endpoints.
	for _, ep := range []string{
		"/terraform/v1/mgmt/login",
		"/terraform/v1/mgmt/contact/query",
		"/terraform/v1/mgmt/ffmpeg/capabilities",
		"/terraform/v1/mgmt/streams/query",
	} {
		req := httptest.NewRequest(http.MethodPost, ep, nil)
		if _, pattern := handler.Handler(req); pattern == "" {
			t.Errorf("route %v not registered", ep)
		}
	}
}
