// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare // import "miniflux.app/v2/internal/reader/cloudflare"

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"miniflux.app/v2/internal/config"
)

var (
	defaultBypass     *Bypass
	defaultBypassOnce sync.Once
)

// Default returns a process-scoped Bypass built from config once.
// Safe for concurrent use; Cache already uses its own locking.
// Clearance entries therefore survive across Create/Refresh feed fetches.
func Default() *Bypass {
	defaultBypassOnce.Do(func() {
		defaultBypass = NewBypassFromConfig()
	})
	return defaultBypass
}

// Policy controls per-request bypass enablement relative to global config.
type Policy struct {
	// FeedOverride: nil = follow global; &true / &false = force (true still requires global enable + URL).
	FeedOverride *bool
}

// Solver obtains Cloudflare clearance cookies for a website URL.
// *Client implements Solver; tests inject fakes.
type Solver interface {
	Solve(ctx context.Context, req SolveRequest) (*SolveResponse, error)
}

// Bypass orchestrates cache lookup, challenge detection, singleflight solve, and one retry.
type Bypass struct {
	client  Solver
	cache   *Cache
	url     string
	enabled bool
	timeout time.Duration
}

// NewBypass constructs a Bypass with the given dependencies.
// A nil cache is replaced with a default in-process cache (DefaultTTL/MaxTTL/Skew).
func NewBypass(solver Solver, cache *Cache, bypassURL string, enabled bool, timeout time.Duration) *Bypass {
	if cache == nil {
		// Skew must be set explicitly: NewCache treats Skew==0 as "no early expiry".
		cache = NewCache(CacheOptions{
			DefaultTTL: DefaultCacheTTL,
			Skew:       DefaultCacheSkew,
			MaxTTL:     DefaultCacheMaxTTL,
		})
	}
	return &Bypass{
		client:  solver,
		cache:   cache,
		url:     strings.TrimSpace(bypassURL),
		enabled: enabled,
		timeout: timeout,
	}
}

// NewBypassFromConfig reads config.Opts and builds a production Bypass.
// Safe when config.Opts is nil (tests): returns a disabled bypass with an empty cache.
func NewBypassFromConfig() *Bypass {
	if config.Opts == nil {
		return NewBypass(nil, nil, "", false, 0)
	}
	bypassURL := config.Opts.CloudflareBypassURL()
	return NewBypass(
		&Client{BaseURL: bypassURL},
		NewCache(CacheOptions{
			DefaultTTL: config.Opts.CloudflareBypassCacheTTL(),
			// Skew must be set explicitly: NewCache treats Skew==0 as "no early expiry".
			Skew:   DefaultCacheSkew,
			MaxTTL: DefaultCacheMaxTTL,
		}),
		bypassURL,
		config.Opts.CloudflareBypassEnabled(),
		config.Opts.CloudflareBypassTimeout(),
	)
}

// Enabled reports whether bypass may run for this policy.
// Formula: URL non-empty && global enabled && feedOverride != false.
func (b *Bypass) Enabled(p Policy) bool {
	if b == nil {
		return false
	}
	if b.url == "" || !b.enabled {
		return false
	}
	if p.FeedOverride != nil && !*p.FeedOverride {
		return false
	}
	return true
}

// DoRequest applies cached clearance when enabled, performs the request via do,
// and on a Cloudflare challenge solves once (singleflight) and retries once.
//
// proxyKey identifies the egress path for the cache ("direct" or redacted proxy URL).
// proxyForSolver is the actual proxy string passed to the solver (may include credentials).
// existingCookie/existingUA are feed-level defaults; empty cookie/ua from do means use builder defaults at the call site.
//
// On solve failure the original challenge response is returned (err == nil) so callers can map it to a localized CF error.
func (b *Bypass) DoRequest(
	ctx context.Context,
	requestURL string,
	proxyKey string,
	proxyForSolver string,
	policy Policy,
	existingCookie string,
	existingUA string,
	do func(cookie, userAgent string) (*http.Response, error),
) (*http.Response, error) {
	if do == nil {
		return nil, fmt.Errorf("cloudflare: do callback is nil")
	}
	if b == nil || !b.Enabled(policy) {
		return do(existingCookie, existingUA)
	}

	cookie, ua := existingCookie, existingUA
	appliedCache := false
	host := hostFromURL(requestURL)
	key := CacheKey(host, proxyKey)

	if ent, ok := b.cache.Get(key); ok {
		cookie = MergeCookies(existingCookie, ParseCookieHeader(ent.CookieHeader))
		if ent.UserAgent != "" {
			ua = ent.UserAgent
		}
		appliedCache = true
	}

	resp, err := do(cookie, ua)
	if err != nil || !IsChallenge(resp) {
		return resp, err
	}

	// Challenge path: drop stale cache entry that was applied for this attempt.
	if appliedCache {
		b.cache.Invalidate(key)
	}

	entry, solveErr := b.cache.GetOrSolve(key, func() (CacheEntry, error) {
		return b.solveToEntry(ctx, requestURL, proxyForSolver)
	})
	if solveErr != nil {
		// Caller maps the original CF response to a localized error.
		return resp, nil
	}

	// Discard first body before retry to avoid connection leaks.
	drainAndClose(resp)

	cookie = MergeCookies(existingCookie, ParseCookieHeader(entry.CookieHeader))
	if entry.UserAgent != "" {
		ua = entry.UserAgent
	}

	resp2, err2 := do(cookie, ua)
	if err2 != nil {
		return resp2, err2
	}
	if IsChallenge(resp2) {
		b.cache.Invalidate(key)
	}
	return resp2, nil
}

func (b *Bypass) solveToEntry(ctx context.Context, requestURL, proxyForSolver string) (CacheEntry, error) {
	if b.client == nil {
		return CacheEntry{}, fmt.Errorf("cloudflare: solver is nil")
	}
	sol, err := b.client.Solve(ctx, SolveRequest{
		WebsiteURL: requestURL,
		Proxy:      proxyForSolver,
		Timeout:    b.timeout,
	})
	if err != nil {
		return CacheEntry{}, err
	}
	if sol == nil || !sol.OK {
		code, desc := "", ""
		if sol != nil {
			code, desc = sol.ErrorCode, sol.ErrorDesc
		}
		return CacheEntry{}, fmt.Errorf("cloudflare: solve failed: %s %s", code, desc)
	}
	if !hasClearanceCookie(sol.Cookies) {
		return CacheEntry{}, fmt.Errorf("cloudflare: solve failed: ERROR_NO_CLEARANCE")
	}
	return CacheEntry{
		CookieHeader: FormatCookieHeader(sol.Cookies),
		UserAgent:    sol.UserAgent,
		ExpiresAt:    sol.ExpiresAt,
	}, nil
}

func hasClearanceCookie(cookies []Cookie) bool {
	for _, c := range cookies {
		if c.Name == "cf_clearance" && c.Value != "" {
			return true
		}
	}
	return false
}

// hostFromURL returns the lowercased hostname for cache keys.
func hostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		// Best-effort fallback for malformed URLs.
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(u.Hostname())
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
