// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cloudflare // import "miniflux.app/v2/internal/reader/cloudflare"

import (
	"sync"
	"time"
)

const (
	// DefaultCacheTTL is the fallback lifetime when ExpiresAt is zero.
	DefaultCacheTTL = 25 * time.Minute
	// DefaultCacheSkew is subtracted from expiry so near-expiry cookies are not reused.
	DefaultCacheSkew = 60 * time.Second
	// DefaultCacheMaxTTL is a hard cap on how long a clearance entry may live.
	DefaultCacheMaxTTL = 24 * time.Hour
)

// CacheEntry holds a solved Cloudflare clearance cookie set and its lifetime.
type CacheEntry struct {
	CookieHeader string
	UserAgent    string
	ExpiresAt    time.Time
}

// CacheOptions configures an in-process clearance cache.
// Skew may be zero (no early expiry). DefaultTTL and MaxTTL use package defaults when <= 0.
type CacheOptions struct {
	DefaultTTL time.Duration
	Skew       time.Duration
	MaxTTL     time.Duration
}

// Cache is a process-local clearance cookie cache with singleflight solve coalescing.
type Cache struct {
	mu         sync.Mutex
	entries    map[string]CacheEntry
	defaultTTL time.Duration
	skew       time.Duration
	maxTTL     time.Duration
	sf         singleflight
}

// NewCache returns a Cache with the given options.
// Zero/negative DefaultTTL and MaxTTL fall back to package defaults.
// Skew of zero is valid and means no early-expiry margin.
func NewCache(opts CacheOptions) *Cache {
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = DefaultCacheTTL
	}
	if opts.MaxTTL <= 0 {
		opts.MaxTTL = DefaultCacheMaxTTL
	}
	if opts.Skew < 0 {
		opts.Skew = DefaultCacheSkew
	}
	return &Cache{
		entries:    make(map[string]CacheEntry),
		defaultTTL: opts.DefaultTTL,
		skew:       opts.Skew,
		maxTTL:     opts.MaxTTL,
	}
}

// CacheKey builds a cache key from host and proxy identity.
// proxyKey should distinguish direct egress from each configured proxy.
func CacheKey(host, proxyKey string) string {
	return host + "\x00" + proxyKey
}

// Get returns a non-expired entry for key.
func (c *Cache) Get(key string) (CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		return CacheEntry{}, false
	}
	if !e.ExpiresAt.After(time.Now()) {
		delete(c.entries, key)
		return CacheEntry{}, false
	}
	return e, true
}

// Set stores entry under key after applying MaxTTL and skew.
// If remaining life after skew is not positive, the entry is not stored.
// Zero ExpiresAt uses DefaultTTL from now.
func (c *Cache) Set(key string, entry CacheEntry) {
	now := time.Now()
	exp := entry.ExpiresAt
	if exp.IsZero() {
		exp = now.Add(c.defaultTTL)
	}
	if max := now.Add(c.maxTTL); exp.After(max) {
		exp = max
	}
	// effective expiry is wall expiry minus skew so near-expiry cookies are never served.
	effective := exp.Add(-c.skew)
	if !effective.After(now) {
		return
	}
	entry.ExpiresAt = effective

	c.mu.Lock()
	c.entries[key] = entry
	c.mu.Unlock()
}

// Invalidate removes key from the cache if present.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// GetOrSolve returns a cached entry or runs solve exactly once per key among concurrent callers.
// The solve result is returned even when it is too short-lived to cache (remaining life < skew).
func (c *Cache) GetOrSolve(key string, solve func() (CacheEntry, error)) (CacheEntry, error) {
	if e, ok := c.Get(key); ok {
		return e, nil
	}

	v, err, _ := c.sf.Do(key, func() (CacheEntry, error) {
		if e, ok := c.Get(key); ok {
			return e, nil
		}
		e, err := solve()
		if err != nil {
			return CacheEntry{}, err
		}
		c.Set(key, e)
		// Return the fresh solve result even if Set declined to store it.
		return e, nil
	})
	return v, err
}

// singleflight coalesces concurrent work for the same key (stdlib-only, no x/sync).
type singleflight struct {
	mu sync.Mutex
	m  map[string]*sfCall
}

type sfCall struct {
	wg  sync.WaitGroup
	val CacheEntry
	err error
}

func (g *singleflight) Do(key string, fn func() (CacheEntry, error)) (v CacheEntry, err error, shared bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*sfCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err, true
	}
	c := new(sfCall)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err, false
}
