// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/reader/cloudflare"
)

func boolPtr(v bool) *bool { return &v }

type fakeSolver struct {
	calls atomic.Int32
	fn    func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error)
}

func (f *fakeSolver) Solve(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx, req)
	}
	return &cloudflare.SolveResponse{
		OK:        true,
		UserAgent: "Solved-UA",
		Cookies: []cloudflare.Cookie{
			{Name: "cf_clearance", Value: "solved"},
		},
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}, nil
}

func challengeResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{
			"Cf-Mitigated": []string{"challenge"},
			"Content-Type": []string{"text/html; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader("cf challenge")),
	}
}

func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newEnabledBypass(solver cloudflare.Solver, cache *cloudflare.Cache) *cloudflare.Bypass {
	if cache == nil {
		cache = cloudflare.NewCache(cloudflare.CacheOptions{
			DefaultTTL: 25 * time.Minute,
			Skew:       60 * time.Second,
		})
	}
	return cloudflare.NewBypass(solver, cache, "http://127.0.0.1:8191", true, 60*time.Second)
}

func TestBypassEnabled(t *testing.T) {
	b := cloudflare.NewBypass(nil, nil, "http://127.0.0.1:8191", true, time.Minute)

	if !b.Enabled(cloudflare.Policy{}) {
		t.Fatal("expected enabled with URL + global on + nil override")
	}
	if !b.Enabled(cloudflare.Policy{FeedOverride: boolPtr(true)}) {
		t.Fatal("expected enabled with feed override true")
	}
	if b.Enabled(cloudflare.Policy{FeedOverride: boolPtr(false)}) {
		t.Fatal("expected disabled with feed override false")
	}

	disabled := cloudflare.NewBypass(nil, nil, "http://127.0.0.1:8191", false, time.Minute)
	if disabled.Enabled(cloudflare.Policy{FeedOverride: boolPtr(true)}) {
		t.Fatal("feed true must not enable when global disabled")
	}

	noURL := cloudflare.NewBypass(nil, nil, "", true, time.Minute)
	if noURL.Enabled(cloudflare.Policy{}) {
		t.Fatal("expected disabled when URL empty")
	}

	var nilBypass *cloudflare.Bypass
	if nilBypass.Enabled(cloudflare.Policy{}) {
		t.Fatal("nil bypass must be disabled")
	}
}

func TestDoRequestDisabledCallsDoOnceNoSolver(t *testing.T) {
	solver := &fakeSolver{}
	b := cloudflare.NewBypass(solver, nil, "", true, time.Minute)

	var doCalls atomic.Int32
	resp, err := b.DoRequest(
		context.Background(),
		"https://www.example.com/feed",
		"direct",
		"",
		cloudflare.Policy{},
		"session=1",
		"Feed-UA",
		func(cookie, ua string) (*http.Response, error) {
			doCalls.Add(1)
			if cookie != "session=1" || ua != "Feed-UA" {
				t.Fatalf("cookie/ua = %q/%q want session=1/Feed-UA", cookie, ua)
			}
			return challengeResponse(), nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d want 403", resp.StatusCode)
	}
	if doCalls.Load() != 1 {
		t.Fatalf("do calls = %d want 1", doCalls.Load())
	}
	if solver.calls.Load() != 0 {
		t.Fatalf("solver calls = %d want 0", solver.calls.Load())
	}
}

func TestDoRequestCFThenSolveOKThen200(t *testing.T) {
	solver := &fakeSolver{
		fn: func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
			if req.WebsiteURL != "https://www.example.com/feed" {
				t.Fatalf("WebsiteURL = %q", req.WebsiteURL)
			}
			if req.Proxy != "http://user:pass@proxy:8080" {
				t.Fatalf("Proxy = %q", req.Proxy)
			}
			if req.Timeout != 60*time.Second {
				t.Fatalf("Timeout = %v", req.Timeout)
			}
			return &cloudflare.SolveResponse{
				OK:        true,
				UserAgent: "Solved-UA",
				Cookies:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "tok"}},
				ExpiresAt: time.Now().Add(30 * time.Minute),
			}, nil
		},
	}
	b := newEnabledBypass(solver, nil)

	var doCalls atomic.Int32
	var cookies []string
	var uas []string
	resp, err := b.DoRequest(
		context.Background(),
		"https://www.example.com/feed",
		"http://proxy:8080",
		"http://user:pass@proxy:8080",
		cloudflare.Policy{},
		"session=keep",
		"Feed-UA",
		func(cookie, ua string) (*http.Response, error) {
			n := doCalls.Add(1)
			cookies = append(cookies, cookie)
			uas = append(uas, ua)
			if n == 1 {
				return challengeResponse(), nil
			}
			return okResponse("rss"), nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", resp.StatusCode)
	}
	if doCalls.Load() != 2 {
		t.Fatalf("do calls = %d want 2", doCalls.Load())
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls = %d want 1", solver.calls.Load())
	}
	if cookies[0] != "session=keep" || uas[0] != "Feed-UA" {
		t.Fatalf("first attempt cookie/ua = %q/%q", cookies[0], uas[0])
	}
	if !strings.Contains(cookies[1], "cf_clearance=tok") || !strings.Contains(cookies[1], "session=keep") {
		t.Fatalf("retry cookie = %q want merged session + clearance", cookies[1])
	}
	if uas[1] != "Solved-UA" {
		t.Fatalf("retry ua = %q want Solved-UA", uas[1])
	}
}

func TestDoRequestCacheHitNoSolverWhen200(t *testing.T) {
	solver := &fakeSolver{}
	cache := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       60 * time.Second,
	})
	key := cloudflare.CacheKey("www.example.com", "direct")
	cache.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=cached",
		UserAgent:    "Cached-UA",
		ExpiresAt:    time.Now().Add(20 * time.Minute),
	})
	b := newEnabledBypass(solver, cache)

	var doCalls atomic.Int32
	resp, err := b.DoRequest(
		context.Background(),
		"https://WWW.Example.com/feed.xml",
		"direct",
		"",
		cloudflare.Policy{},
		"session=1",
		"Feed-UA",
		func(cookie, ua string) (*http.Response, error) {
			doCalls.Add(1)
			if !strings.Contains(cookie, "cf_clearance=cached") || !strings.Contains(cookie, "session=1") {
				t.Fatalf("cookie = %q want merge of session + cached clearance", cookie)
			}
			if ua != "Cached-UA" {
				t.Fatalf("ua = %q want Cached-UA", ua)
			}
			return okResponse("rss"), nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", resp.StatusCode)
	}
	if doCalls.Load() != 1 {
		t.Fatalf("do calls = %d want 1", doCalls.Load())
	}
	if solver.calls.Load() != 0 {
		t.Fatalf("solver calls = %d want 0", solver.calls.Load())
	}
}

func TestDoRequestCacheHitStillCFInvalidatesAndSolves(t *testing.T) {
	solver := &fakeSolver{
		fn: func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
			return &cloudflare.SolveResponse{
				OK:        true,
				UserAgent: "Fresh-UA",
				Cookies:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "fresh"}},
				ExpiresAt: time.Now().Add(30 * time.Minute),
			}, nil
		},
	}
	cache := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       60 * time.Second,
	})
	key := cloudflare.CacheKey("www.example.com", "direct")
	cache.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=stale",
		UserAgent:    "Stale-UA",
		ExpiresAt:    time.Now().Add(20 * time.Minute),
	})
	b := newEnabledBypass(solver, cache)

	var doCalls atomic.Int32
	var seenCookies []string
	resp, err := b.DoRequest(
		context.Background(),
		"https://www.example.com/feed",
		"direct",
		"",
		cloudflare.Policy{},
		"",
		"Feed-UA",
		func(cookie, ua string) (*http.Response, error) {
			n := doCalls.Add(1)
			seenCookies = append(seenCookies, cookie)
			if n == 1 {
				if !strings.Contains(cookie, "cf_clearance=stale") {
					t.Fatalf("first cookie = %q want stale clearance", cookie)
				}
				return challengeResponse(), nil
			}
			if !strings.Contains(cookie, "cf_clearance=fresh") {
				t.Fatalf("retry cookie = %q want fresh clearance", cookie)
			}
			if ua != "Fresh-UA" {
				t.Fatalf("retry ua = %q want Fresh-UA", ua)
			}
			return okResponse("rss"), nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", resp.StatusCode)
	}
	if doCalls.Load() != 2 {
		t.Fatalf("do calls = %d want 2", doCalls.Load())
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls = %d want 1", solver.calls.Load())
	}
	// Stale entry must have been replaced; a subsequent Get should see fresh.
	if ent, ok := cache.Get(key); !ok || !strings.Contains(ent.CookieHeader, "cf_clearance=fresh") {
		t.Fatalf("cache after solve = (%v, %v) want fresh", ent, ok)
	}
	_ = seenCookies
}

func TestDoRequestSolveErrorReturnsFirstCFResponse(t *testing.T) {
	solver := &fakeSolver{
		fn: func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
			return nil, errors.New("solver down")
		},
	}
	b := newEnabledBypass(solver, nil)

	var doCalls atomic.Int32
	first := challengeResponse()
	resp, err := b.DoRequest(
		context.Background(),
		"https://www.example.com/feed",
		"direct",
		"",
		cloudflare.Policy{},
		"",
		"",
		func(cookie, ua string) (*http.Response, error) {
			doCalls.Add(1)
			return first, nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v (want nil with original CF resp)", err)
	}
	if resp != first {
		t.Fatal("expected original CF response on solve failure")
	}
	if doCalls.Load() != 1 {
		t.Fatalf("do calls = %d want 1", doCalls.Load())
	}
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls = %d want 1", solver.calls.Load())
	}
}

func TestDoRequestStillCFAfterSolveInvalidates(t *testing.T) {
	solver := &fakeSolver{
		fn: func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
			return &cloudflare.SolveResponse{
				OK:        true,
				UserAgent: "Solved-UA",
				Cookies:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "tok"}},
				ExpiresAt: time.Now().Add(30 * time.Minute),
			}, nil
		},
	}
	cache := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       60 * time.Second,
	})
	b := newEnabledBypass(solver, cache)
	key := cloudflare.CacheKey("www.example.com", "direct")

	var doCalls atomic.Int32
	resp, err := b.DoRequest(
		context.Background(),
		"https://www.example.com/feed",
		"direct",
		"",
		cloudflare.Policy{},
		"",
		"",
		func(cookie, ua string) (*http.Response, error) {
			doCalls.Add(1)
			return challengeResponse(), nil
		},
	)
	if err != nil {
		t.Fatalf("DoRequest error: %v", err)
	}
	if !cloudflare.IsChallenge(resp) {
		t.Fatal("expected CF response after failed retry")
	}
	if doCalls.Load() != 2 {
		t.Fatalf("do calls = %d want 2", doCalls.Load())
	}
	if _, ok := cache.Get(key); ok {
		t.Fatal("expected cache invalidated after still-CF retry")
	}
}

func TestNewBypassFromConfigNilSafe(t *testing.T) {
	prev := config.Opts
	config.Opts = nil
	t.Cleanup(func() { config.Opts = prev })

	b := cloudflare.NewBypassFromConfig()
	if b == nil {
		t.Fatal("NewBypassFromConfig returned nil")
	}
	if b.Enabled(cloudflare.Policy{}) {
		t.Fatal("nil Opts bypass should not be enabled")
	}
}

// TestNewBypassNilCacheAppliesDefaultSkew verifies that constructing Bypass with a nil
// cache opts into DefaultCacheSkew (60s). Near-expiry solve results must not be stored,
// so a second challenge forces another solve.
func TestNewBypassNilCacheAppliesDefaultSkew(t *testing.T) {
	solver := &fakeSolver{
		fn: func(ctx context.Context, req cloudflare.SolveRequest) (*cloudflare.SolveResponse, error) {
			return &cloudflare.SolveResponse{
				OK:        true,
				UserAgent: "Solved-UA",
				Cookies:   []cloudflare.Cookie{{Name: "cf_clearance", Value: "hot"}},
				// Within DefaultCacheSkew (60s) → Set must refuse to cache.
				ExpiresAt: time.Now().Add(30 * time.Second),
			}, nil
		},
	}
	// nil cache → NewBypass must install defaults including DefaultCacheSkew.
	b := cloudflare.NewBypass(solver, nil, "http://127.0.0.1:8191", true, time.Minute)

	doCFThenOK := func() {
		t.Helper()
		var doCalls atomic.Int32
		resp, err := b.DoRequest(
			context.Background(),
			"https://www.example.com/feed",
			"direct",
			"",
			cloudflare.Policy{},
			"",
			"",
			func(cookie, ua string) (*http.Response, error) {
				n := doCalls.Add(1)
				if n == 1 {
					return challengeResponse(), nil
				}
				return okResponse("rss"), nil
			},
		)
		if err != nil {
			t.Fatalf("DoRequest error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d want 200", resp.StatusCode)
		}
		if doCalls.Load() != 2 {
			t.Fatalf("do calls = %d want 2", doCalls.Load())
		}
	}

	doCFThenOK()
	if solver.calls.Load() != 1 {
		t.Fatalf("solver calls after first request = %d want 1", solver.calls.Load())
	}

	// Second challenge must solve again: near-expiry entry was not cached under default skew.
	doCFThenOK()
	if solver.calls.Load() != 2 {
		t.Fatalf("solver calls after second request = %d want 2 (near-expiry must not be cached)", solver.calls.Load())
	}
}
