// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
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

func TestService_ResolveTutorialEntry_BilibiliFallback(t *testing.T) {
	ctx := context.Background()
	entry := &TutorialEntry{
		ID:     "BV1844y1L7dL",
		Source: "bilibili",
		Author: "SRS",
	}

	resolveTutorialEntry(ctx, entry, func(context.Context, string) (map[string]interface{}, error) {
		return nil, errors.New("bilibili rate limited")
	})

	if entry.Media != "Bilibili" {
		t.Fatalf("invalid media %v", entry.Media)
	}
	if entry.Link != "https://www.bilibili.com/video/BV1844y1L7dL" {
		t.Fatalf("invalid link %v", entry.Link)
	}
	if entry.Title != "前往观看" {
		t.Fatalf("invalid fallback title %v", entry.Title)
	}
}
