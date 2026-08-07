// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"miniflux.app/v2/internal/reader/cloudflare"
)

func TestClientSolveSuccess(t *testing.T) {
	var gotMethod, gotPath, gotCT string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"websiteURL": "https://example.com/feed",
			"finalURL":   "https://example.com/feed",
			"userAgent":  "Mozilla/5.0 Test",
			"cookies": []map[string]any{
				{
					"name":     "cf_clearance",
					"value":    "abc123",
					"domain":   ".example.com",
					"path":     "/",
					"expires":  1770000000,
					"httpOnly": true,
					"secure":   true,
					"sameSite": "None",
				},
				{"name": "other", "value": "x"},
			},
			"expiresAt": 1770000000,
		})
	}))
	defer server.Close()

	client := &cloudflare.Client{
		BaseURL:    server.URL + "/",
		HTTPClient: server.Client(),
	}
	resp, err := client.Solve(context.Background(), cloudflare.SolveRequest{
		WebsiteURL: "https://example.com/feed",
		Proxy:      "http://user:pass@proxy:8080",
		Timeout:    60 * time.Second,
		UserAgent:  "Miniflux-Test/1.0",
	})
	if err != nil {
		t.Fatalf("Solve() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q want POST", gotMethod)
	}
	if gotPath != "/v1/clearance" {
		t.Fatalf("path = %q want /v1/clearance", gotPath)
	}
	if !strings.Contains(gotCT, "application/json") {
		t.Fatalf("Content-Type = %q want application/json", gotCT)
	}
	if gotBody["websiteURL"] != "https://example.com/feed" {
		t.Fatalf("body websiteURL = %v", gotBody["websiteURL"])
	}
	if gotBody["proxy"] != "http://user:pass@proxy:8080" {
		t.Fatalf("body proxy = %v", gotBody["proxy"])
	}
	if gotBody["userAgent"] != "Miniflux-Test/1.0" {
		t.Fatalf("body userAgent = %v", gotBody["userAgent"])
	}
	timeoutSec, ok := gotBody["timeoutSec"].(float64)
	if !ok || timeoutSec != 60 {
		t.Fatalf("body timeoutSec = %v want 60", gotBody["timeoutSec"])
	}

	if !resp.OK {
		t.Fatalf("OK = false want true")
	}
	if resp.UserAgent != "Mozilla/5.0 Test" {
		t.Fatalf("UserAgent = %q", resp.UserAgent)
	}
	if resp.FinalURL != "https://example.com/feed" {
		t.Fatalf("FinalURL = %q", resp.FinalURL)
	}
	if !resp.ExpiresAt.Equal(time.Unix(1770000000, 0)) {
		t.Fatalf("ExpiresAt = %v want unix 1770000000", resp.ExpiresAt)
	}
	if len(resp.Cookies) != 2 {
		t.Fatalf("Cookies len = %d want 2: %#v", len(resp.Cookies), resp.Cookies)
	}
	if resp.Cookies[0] != (cloudflare.Cookie{Name: "cf_clearance", Value: "abc123"}) {
		t.Fatalf("Cookies[0] = %#v", resp.Cookies[0])
	}
	if resp.Cookies[1] != (cloudflare.Cookie{Name: "other", Value: "x"}) {
		t.Fatalf("Cookies[1] = %#v", resp.Cookies[1])
	}
	if resp.ErrorCode != "" || resp.ErrorDesc != "" {
		t.Fatalf("unexpected error fields: code=%q desc=%q", resp.ErrorCode, resp.ErrorDesc)
	}
}

func TestClientSolveBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":               false,
			"errorCode":        "ERROR_NO_CLEARANCE",
			"errorDescription": "cf_clearance not observed within timeout",
		})
	}))
	defer server.Close()

	client := &cloudflare.Client{BaseURL: server.URL, HTTPClient: server.Client()}
	resp, err := client.Solve(context.Background(), cloudflare.SolveRequest{
		WebsiteURL: "https://example.com/feed",
	})
	if err != nil {
		t.Fatalf("Solve() transport error = %v; business ok:false should not be an error", err)
	}
	if resp.OK {
		t.Fatal("OK = true want false")
	}
	if resp.ErrorCode != "ERROR_NO_CLEARANCE" {
		t.Fatalf("ErrorCode = %q", resp.ErrorCode)
	}
	if resp.ErrorDesc != "cf_clearance not observed within timeout" {
		t.Fatalf("ErrorDesc = %q", resp.ErrorDesc)
	}
}

func TestClientSolveServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &cloudflare.Client{BaseURL: server.URL, HTTPClient: server.Client()}
	resp, err := client.Solve(context.Background(), cloudflare.SolveRequest{
		WebsiteURL: "https://example.com/feed",
	})
	if err == nil {
		t.Fatalf("Solve() error = nil want non-nil; resp=%#v", resp)
	}
	if resp != nil {
		t.Fatalf("resp = %#v want nil on transport/status error", resp)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v; want status 500 mentioned", err)
	}
}

func TestClientSolveContextTimeout(t *testing.T) {
	// Unblock the handler if the request context is not cancelled by the client abort.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client := &cloudflare.Client{BaseURL: server.URL, HTTPClient: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	resp, err := client.Solve(ctx, cloudflare.SolveRequest{
		WebsiteURL: "https://example.com/feed",
	})
	if err == nil {
		t.Fatalf("Solve() error = nil want context deadline; resp=%#v", resp)
	}
	if resp != nil {
		t.Fatalf("resp = %#v want nil on context error", resp)
	}
	if ctx.Err() == nil && !strings.Contains(err.Error(), "context") {
		// Accept either context.DeadlineExceeded wrapped by http.Client or explicit context text.
		t.Fatalf("error = %v; want context cancellation/timeout", err)
	}
}
