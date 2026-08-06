// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miniflux.app/v2/internal/reader/cloudflare"
)

func newTestCache() *cloudflare.Cache {
	return cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       60 * time.Second,
		MaxTTL:     24 * time.Hour,
	})
}

func TestCacheKeyIsolatesProxy(t *testing.T) {
	a := cloudflare.CacheKey("www.example.com", "direct")
	b := cloudflare.CacheKey("www.example.com", "proxy-1")
	c := cloudflare.CacheKey("other.example.com", "direct")

	if a == b {
		t.Fatalf("expected different keys for different proxyKey, got %q", a)
	}
	if a == c {
		t.Fatalf("expected different keys for different host, got %q", a)
	}
	if a == "" {
		t.Fatal("CacheKey returned empty string")
	}
}

func TestCacheSetGetHit(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")
	want := cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=abc",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}
	c.Set(key, want)

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit before expiry")
	}
	if got.CookieHeader != want.CookieHeader {
		t.Fatalf("CookieHeader=%q want %q", got.CookieHeader, want.CookieHeader)
	}
	if got.UserAgent != want.UserAgent {
		t.Fatalf("UserAgent=%q want %q", got.UserAgent, want.UserAgent)
	}
}

func TestCacheSetPastExpiryMiss(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=abc",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss for entry with ExpiresAt in the past")
	}
}

func TestCacheSkewAndInvalidate(t *testing.T) {
	c := newTestCache()
	host := "www.example.com"
	key := cloudflare.CacheKey(host, "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=abc",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(30 * time.Second), // within skew → should not be stored as usable
	})
	// After Set applies skew, either entry not stored or already expired for Get
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss when remaining life < skew")
	}
}

func TestCacheDifferentProxyKeyIsolated(t *testing.T) {
	c := newTestCache()
	keyDirect := cloudflare.CacheKey("www.example.com", "direct")
	keyProxy := cloudflare.CacheKey("www.example.com", "http://proxy:8080")

	c.Set(keyDirect, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=direct",
		UserAgent:    "UA-direct",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})

	if _, ok := c.Get(keyProxy); ok {
		t.Fatal("expected miss for different proxyKey")
	}
	if e, ok := c.Get(keyDirect); !ok || e.CookieHeader != "cf_clearance=direct" {
		t.Fatalf("expected direct key hit, got ok=%v entry=%+v", ok, e)
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=abc",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})
	if _, ok := c.Get(key); !ok {
		t.Fatal("expected hit before invalidate")
	}
	c.Invalidate(key)
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestCacheMaxTTLCap(t *testing.T) {
	// Use a short MaxTTL so we can observe the cap without waiting 24h.
	c := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       0,
		MaxTTL:     2 * time.Second,
	})
	key := cloudflare.CacheKey("www.example.com", "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=long",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(24 * time.Hour), // far beyond MaxTTL
	})

	if _, ok := c.Get(key); !ok {
		t.Fatal("expected hit immediately after Set with capped TTL")
	}

	// After MaxTTL (+ tiny buffer), entry must miss.
	time.Sleep(2200 * time.Millisecond)
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss after MaxTTL cap elapsed")
	}
}

func TestCacheDefaultTTLWhenExpiresAtZero(t *testing.T) {
	c := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 2 * time.Second,
		Skew:       0,
		MaxTTL:     24 * time.Hour,
	})
	key := cloudflare.CacheKey("www.example.com", "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=def",
		UserAgent:    "UA",
		// ExpiresAt zero → DefaultTTL
	})
	if _, ok := c.Get(key); !ok {
		t.Fatal("expected hit when ExpiresAt is zero (DefaultTTL applied)")
	}
	time.Sleep(2200 * time.Millisecond)
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss after DefaultTTL elapsed")
	}
}

func TestGetOrSolveSingleflight(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")

	var calls atomic.Int32
	solve := func() (cloudflare.CacheEntry, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // force overlap
		return cloudflare.CacheEntry{
			CookieHeader: "cf_clearance=once",
			UserAgent:    "UA",
			ExpiresAt:    time.Now().Add(10 * time.Minute),
		}, nil
	}

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	results := make(chan cloudflare.CacheEntry, n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			e, err := c.GetOrSolve(key, solve)
			if err != nil {
				errs <- err
				return
			}
			results <- e
		}()
	}
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("GetOrSolve error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("solve called %d times, want 1", got)
	}
	count := 0
	for e := range results {
		count++
		if e.CookieHeader != "cf_clearance=once" {
			t.Fatalf("unexpected CookieHeader %q", e.CookieHeader)
		}
	}
	if count != n {
		t.Fatalf("got %d results, want %d", count, n)
	}

	// Subsequent GetOrSolve should hit cache without solving again.
	e, err := c.GetOrSolve(key, solve)
	if err != nil {
		t.Fatalf("second GetOrSolve: %v", err)
	}
	if e.CookieHeader != "cf_clearance=once" {
		t.Fatalf("CookieHeader=%q", e.CookieHeader)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("solve called again after cache fill: %d", got)
	}
}

func TestGetOrSolvePropagatesError(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")
	wantErr := errors.New("solve failed")

	_, err := c.GetOrSolve(key, func() (cloudflare.CacheEntry, error) {
		return cloudflare.CacheEntry{}, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
	if _, ok := c.Get(key); ok {
		t.Fatal("expected no cache entry after solve error")
	}
}

func TestGetOrSolveReturnsEntryEvenIfTooShortToCache(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")

	e, err := c.GetOrSolve(key, func() (cloudflare.CacheEntry, error) {
		return cloudflare.CacheEntry{
			CookieHeader: "cf_clearance=hot",
			UserAgent:    "UA",
			ExpiresAt:    time.Now().Add(30 * time.Second), // within skew
		}, nil
	})
	if err != nil {
		t.Fatalf("GetOrSolve: %v", err)
	}
	if e.CookieHeader != "cf_clearance=hot" {
		t.Fatalf("CookieHeader=%q", e.CookieHeader)
	}
	// Hot cookies for immediate retry are not held by cache.
	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss: short-lived solve result must not be cached")
	}
}

func TestGetOrSolvePanicUnblocksWaiters(t *testing.T) {
	c := newTestCache()
	key := cloudflare.CacheKey("www.example.com", "direct")

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			_ = recover()
		}()
		_, _ = c.GetOrSolve(key, func() (cloudflare.CacheEntry, error) {
			panic("boom")
		})
	}()

	// Waiter must not hang after the owner panics.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		_, err := c.GetOrSolve(key, func() (cloudflare.CacheEntry, error) {
			return cloudflare.CacheEntry{
				CookieHeader: "cf_clearance=after-panic",
				UserAgent:    "UA",
				ExpiresAt:    time.Now().Add(10 * time.Minute),
			}, nil
		})
		if err != nil {
			t.Errorf("waiter GetOrSolve error: %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("owner did not finish after panic")
	}
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter blocked after panicking solve; Done not panic-safe")
	}
}
