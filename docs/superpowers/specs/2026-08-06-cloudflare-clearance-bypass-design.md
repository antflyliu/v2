# Design: Miniflux Cloudflare Clearance Bypass via camoufox-turnstile

**Date:** 2026-08-06  
**Status:** Draft for user review  
**Scope:** Extend `camoufox-turnstile` with Managed/JS Challenge clearance; add a generic miniflux fetcher-layer client/cache/retry. No code in this document.

## 1. Problem

Self-hosted miniflux fetches feeds and scrapes article pages over plain HTTP. When the egress IP is unclean, sites protected by Cloudflare Managed / JS Challenge return an interstitial (commonly `403` + `cf-mitigated: challenge` + HTML) instead of RSS/HTML content. Miniflux already **detects** this (`internal/reader/fetcher/response_handler.go`) but only surfaces `error.http_cloudflare_challenge` — it does not recover.

An existing local service, `camoufox-turnstile` (`D:\WORKSPACE\ai-space\grok-register\services\camoufox-turnstile`), already runs Camoufox with a browser pool, proxy precedence, and a YesCaptcha-shaped Turnstile **token** API. That token API is **not** sufficient for feed GET: RSS clients need `cf_clearance` (and matching User-Agent), not a Turnstile response token.

## 2. Goals and non-goals

### Goals

1. Fully automated recovery: no human browser interaction.
2. Headless Camoufox may run **inside** camoufox-turnstile only.
3. Miniflux never embeds a browser; it only calls HTTP APIs and retries with cookies.
4. Extend **existing** camoufox-turnstile (not a new service).
5. Global enable + per-feed override.
6. In-process cache keyed by host + proxy (no DB persistence of clearance).
7. v1 coverage: feed fetch + scraper full-text; architecture reusable for more fetcher callers later.
8. Sticky proxy: clearance solve uses the same egress proxy as the miniflux request.

### Non-goals

- Pure protocol / no-browser Cloudflare solving.
- Returning full page body from the solver as the primary path.
- Persisting clearance into `feeds.cookie` / DB.
- Auto proxy rotation inside a single solve.
- CI against live Cloudflare.
- Changing RSS parse or readability logic.
- Replacing or breaking existing Turnstile `createTask` / `getTaskResult` for grok-register.

## 3. Decisions (locked)

| Topic | Decision |
|-------|----------|
| Human browser use | None |
| Solver runtime | Headless Camoufox inside camoufox-turnstile |
| Product approach | Clearance solve + HTTP retry (not browser-returned body) |
| Service change | Extend camoufox-turnstile |
| API shape | **Independent** `POST /v1/clearance` (keep YesCaptcha endpoints for Turnstile) |
| Miniflux switch | Global config + per-feed tri-state override |
| Cache | Process memory, key = host + proxyKey |
| v1 paths | Feed create/refresh + scraper |
| Mount point | Generic layer beside/in fetcher; only feed+scraper wire HandleChallenge in v1 |
| Proxy | Task/request proxy wins; same proxy on retry |
| Sync vs async solver API | Synchronous `POST /v1/clearance` for v1 |

## 4. Architecture

```
┌──────────────── miniflux ─────────────────┐
│  RefreshFeed / CreateFeed / ScrapeWebsite │
│           │                               │
│           ▼                               │
│  fetcher.RequestBuilder                   │
│    ApplyCached(cookie/ua)                 │
│    ExecuteRequest                         │
│           │                               │
│           ▼                               │
│  ChallengeDetector                        │
│    no  → normal LocalizedError / body     │
│    yes → ClearanceCache                   │
│            miss/stale → ClearanceClient   │
│           │                               │
│           ▼ HTTP                          │
│  inject Cookie + UA, retry once           │
└───────────────────────────────────────────┘
                    │
                    ▼
┌──────── camoufox-turnstile ───────────────┐
│  POST /v1/clearance   (new)               │
│  POST /createTask     (unchanged)         │
│  POST /getTaskResult  (unchanged)         │
│           │                               │
│  shared queue + browser pool              │
│           │                               │
│  Camoufox: goto → wait cf_clearance       │
│           │                               │
│  { cookies, userAgent, expiresAt }        │
└───────────────────────────────────────────┘
```

**Boundary rules**

1. camoufox owns “pass challenge”; miniflux owns “detect / cache / retry”.
2. Turnstile token path and clearance path share the engine, not the public contract.
3. One logical miniflux request: ≤1 solver call and ≤1 business HTTP retry after solve (see §7).

## 5. camoufox-turnstile: clearance API

### 5.1 Endpoint

`POST /v1/clearance` — synchronous; blocks until ready, error, or timeout.

**Request**

```json
{
  "websiteURL": "https://example.com/feed",
  "proxy": "http://user:pass@host:port",
  "timeoutSec": 60,
  "userAgent": ""
}
```

| Field | Required | Notes |
|-------|----------|--------|
| `websiteURL` | yes | URL that triggered the challenge |
| `proxy` | no | Sticky proxy; overrides service `config.proxy` |
| `timeoutSec` | no | Caps solve wall time |
| `userAgent` | no | Optional pin; empty → Camoufox UA returned in response |

**Success**

```json
{
  "ok": true,
  "websiteURL": "https://example.com/feed",
  "finalURL": "https://example.com/feed",
  "userAgent": "Mozilla/5.0 ...",
  "cookies": [
    {
      "name": "cf_clearance",
      "value": "...",
      "domain": ".example.com",
      "path": "/",
      "expires": 1770000000,
      "httpOnly": true,
      "secure": true,
      "sameSite": "None"
    }
  ],
  "expiresAt": 1770000000
}
```

- `cookies` must include `cf_clearance` when `ok=true`.
- Other same-site CF cookies may be included.
- `expiresAt`: min of usable cookie expiries, or service conservative default if browser omits expiry (see §7).
- HTTP 200 with body for application outcomes preferred for client simplicity; transport failures may be non-200.

**Failure body**

```json
{
  "ok": false,
  "errorCode": "ERROR_NO_CLEARANCE",
  "errorDescription": "cf_clearance not observed within timeout"
}
```

| errorCode | Meaning |
|-----------|---------|
| `ERROR_BAD_REQUEST` | Invalid URL / JSON |
| `ERROR_PROXY_REQUIRED` | No proxy and `allow_proxyless=false` |
| `ERROR_QUEUE_FULL` | Work queue full |
| `ERROR_SLOT_TIMEOUT` | Browser slot not acquired in time |
| `ERROR_GOTO_FAILED` | Navigation failed |
| `ERROR_NO_CLEARANCE` | No `cf_clearance` (bad IP, harder challenge, etc.) |
| `ERROR_TIMEOUT` | Overall timeout |
| `ERROR_SOLVER` | Unexpected internal error |

### 5.2 Solve semantics

1. Resolve proxy: **request proxy > config.proxy > proxyless** (same precedence as Turnstile tasks).
2. Acquire browser slot (shared pool with Turnstile workers).
3. `page.goto(websiteURL)` with configured goto timeout.
4. Success when cookie jar contains **`cf_clearance`** for the relevant domain (soft checks: challenge interstitial gone — log-only aids, not sole success criteria).
5. Export cookies + actual User-Agent; compute `expiresAt`.
6. Do not parse feed/HTML content; do not click site chrome beyond what is needed for the CF interstitial; do not rotate proxy on failure.

### 5.3 Concurrency and logging

- Reuse `max_browsers`, queue size, phase logging; tag clearance work with `mode=clearance`.
- Do not log full cookie values at info level (truncate in debug if needed).
- Default bind `127.0.0.1` unchanged.

### 5.4 Compatibility

Existing `POST /createTask` and `POST /getTaskResult` remain the Turnstile token API for grok-register. No breaking change required for registration flows.

## 6. miniflux: generic Cloudflare layer

### 6.1 Package layout (proposed)

```
internal/reader/cloudflare/
  detector.go    # challenge detection
  cache.go       # in-process cache + singleflight
  client.go      # HTTP client for POST /v1/clearance
  bypass.go      # orchestration
```

Optional: thin hooks on `fetcher.RequestBuilder` / `ResponseHandler` that delegate to this package so feed and scraper share one path.

### 6.2 Configuration

**Global**

| Setting | Default | Meaning |
|---------|---------|---------|
| `CLOUDFLARE_BYPASS_URL` | empty | Solver base URL; empty disables |
| `CLOUDFLARE_BYPASS_ENABLED` | `false` | Global master switch |
| `CLOUDFLARE_BYPASS_TIMEOUT` | `60s` | Client timeout to solver |
| `CLOUDFLARE_BYPASS_CACHE_TTL` | `25m` | Fallback TTL when `expiresAt` missing/unusable |
| `CLOUDFLARE_BYPASS_MAX_RETRIES` | `1` | Business HTTP retries after a successful solve (fixed at 1 for v1) |

**Per-feed tri-state** (name illustrative: `cloudflare_bypass`)

| Value | Effect |
|-------|--------|
| unset / null | Follow global |
| true | Enable for this feed **only if** global URL set **and** global enabled |
| false | Never bypass for this feed |

Enable formula:

```
enabled = (BypassURL != "") && GlobalEnabled && (feedOverride != false)
```

Global remains the master switch: feed `true` cannot enable bypass when global is off.

### 6.3 Detection

**v1 hard trigger (authoritative):**

- Status `403`
- Header `cf-mitigated: challenge` (case-insensitive)
- `Content-Type` starts with `text/html`

This matches existing `ResponseHandler.isCloudflareChallenge`. Logic should live in one place (detector) and be reused by the handler to avoid drift.

**Soft signals (optional, off or log-only in v1):** `Server: cloudflare`, body/title markers such as “Just a moment”. Not required to open solver by default (reduces false positives on real 403s).

### 6.4 Orchestration (feed + scraper)

```
builder configured with feed cookie/ua/proxy (existing)
proxyKey = actual proxy used for this request (or "direct")
bypass.ApplyCached(url, proxyKey, builder)   // merge cached Cookie + UA if fresh

resp1 = builder.ExecuteRequest(url)

if !detector.IsChallenge(resp1) || !bypass.Enabled(policy):
    continue existing path

// challenge path
if cache had been applied for this attempt:
    cache.Invalidate(key)   // treat as stale/invalid under this proxy

result = bypass.SolveOnce(url, proxy)  // singleflight per cache key
if result failed:
    return existing Cloudflare localized error (log solver code)

apply result cookies/ua to builder (merge with feed cookies; solver names win)
resp2 = builder.ExecuteRequest(url)   // MUST reuse same proxyKey — no rotator step

if detector.IsChallenge(resp2):
    cache.Invalidate(key)
    return Cloudflare error

cache.Store(key, cookies, ua, expiry)
continue success path with resp2
```

**Call sites v1:** `handler.CreateFeed`, `handler.RefreshFeed`, `scraper.ScrapeWebsite`.  
**Not v1:** subscription discovery, icon finder, media proxy, etc.

### 6.5 Cookie and UA merge

- Serialize solver cookies into a `Cookie` request header.
- Merge with existing feed `Cookie`: same cookie **name** → solver value wins; other names preserved.
- Retry **must** use solver `userAgent` when present (clearance is UA-bound). Do **not** persist UA or cookies to DB in v1.

### 6.6 Proxy alignment

- `proxyKey` and solver `proxy` come from the **same** resolved proxy as the failing request (feed `ProxyURL`, app proxy, or the rotator selection already chosen for that attempt).
- On retry after solve, **do not** call `GetNextProxy()` again.
- Different proxies for the same host use different cache keys.

## 7. Cookie / clearance lifecycle (expiry and invalidation)

This section is normative for cache behavior.

### 7.1 Sources of lifetime

| Source | Priority | Use |
|--------|----------|-----|
| Solver `expiresAt` | 1 | Absolute expiry for cache entry |
| `cf_clearance` cookie `expires` / `max-age` from solver payload | 2 | If `expiresAt` absent, derive it |
| `CLOUDFLARE_BYPASS_CACHE_TTL` (default 25m) | 3 | Fallback when browser/solver omit expiry |
| Safety skew | always | Store as `expiresAt - skew` (recommended skew **60s**) so clients refresh before hard CF expiry |

Never cache an entry without an effective `ExpiresAt` after applying fallback + skew.

### 7.2 Cache key and value

```
key   = normalizeHost(requestURL) + "\0" + proxyKey
value = {
  CookieHeader string,    // ready-to-send Cookie header (or structured cookies)
  UserAgent    string,
  ExpiresAt    time.Time, // after skew
  StoredAt     time.Time,
  SourceURL    string,    // optional diagnostics
}
```

`normalizeHost`: registrable/host form consistent with how cookies are scoped for lookup (at minimum hostname lowercased; document exact helper in implementation).

### 7.3 When to treat cookies as dead

| Event | Action |
|-------|--------|
| `now >= ExpiresAt` | Miss; do not ApplyCached; solve on next challenge |
| ApplyCached used and response still CF challenge | **Invalidate** key immediately; solve once with budget |
| Solve ok but retry still CF | **Invalidate**; fail request (no second solve in same logical request) |
| Solver returns `ok=true` without `cf_clearance` | Treat as `ERROR_NO_CLEARANCE`; do not cache |
| Process restart | Memory cache empty (by design) |
| Global bypass disabled | Do not read/write cache for new work |
| Feed override false | Skip bypass; leave cache entries for other feeds/hosts intact |

### 7.4 Proactive vs reactive refresh

- **v1: reactive only.** No background refresher thread.
- Freshness is enforced on read (`ApplyCached` checks `ExpiresAt`) and on challenge after use (invalidate + optional single solve).
- Rationale: clearance is IP/UA/proxy bound; proactive refresh without traffic wastes browser slots and can mint cookies that are never used.

### 7.5 Singleflight and stampedes

- Concurrent challenge handling for the same `key` shares one in-flight solve.
- Waiters either apply the shared result or observe the shared error.
- Failed solves do not populate cache; brief negative-cache is **optional** and **not** required in v1 (avoid hiding recovery after proxy fix). If added later, use short TTL (e.g. 30–60s) and explicit error class only for `ERROR_QUEUE_FULL` / hard IP failures.

### 7.6 Clock and TTL edge cases

- If solver `expiresAt` is in the past or within skew window → do not cache; caller may still try one business retry with the just-returned cookies in the **same** HandleChallenge (cookies are “hot”), but the next request must solve again.
- If cookie expiry is session-only (no expires) → use `CLOUDFLARE_BYPASS_CACHE_TTL` only.
- Cap maximum cache TTL (recommended hard cap **24h**) even if cookie claims longer, to limit blast radius of stolen/stale entries in memory.

### 7.7 End-to-end lifetime flow

```
[request]
   │
   ├─ cache lookup
   │    ├─ miss / expired ──────────────────────────────┐
   │    └─ hit → set Cookie+UA                          │
   ▼                                                    │
 business HTTP                                          │
   │                                                    │
   ├─ not CF → done (cache untouched)                   │
   └─ CF ───────────────────────────────────────────────┤
        │                                               │
        ├─ if headers came from cache → Invalidate      │
        ├─ SolveOnce (singleflight) ◄───────────────────┘
        │     fail → CF error, no cache write
        │     ok (hot cookies) → retry HTTP same proxy
        │            ├─ success → Store(expiry-skew)
        │            └─ still CF → Invalidate → CF error
```

## 8. Error handling summary

| Scenario | Behavior | User-facing |
|----------|----------|-------------|
| Bypass disabled | No solver | Existing CF error |
| Feed override false | No solver | Existing CF error |
| Not a CF challenge | No solver | Existing status mapping |
| Cache hit, retry OK | No solver | Success |
| Cache hit, still CF | Invalidate + one solve + one retry | Success or CF error |
| Solver failure | No business retry with fake cookies | CF error; slog includes `errorCode` |
| Solver OK, retry OK | Store cache | Success |
| Solver OK, still CF | Invalidate | CF error |

Do not invent a new primary locale string in v1 unless product asks; keep `error.http_cloudflare_challenge` and put solver detail in structured logs.

## 9. Observability

Miniflux slog fields:

- `component=cloudflare_bypass`
- `event=challenge_detected|cache_hit|cache_miss|cache_expire|cache_invalidate|solve_ok|solve_err|retry_ok|retry_fail`
- `host`, `proxy` (redacted), `feed_id` when known, `error_code`, `duration_ms`

camoufox: existing `phase=*` lines plus `mode=clearance`.

## 10. Testing

### camoufox-turnstile (offline)

- Mock browser session that injects `cf_clearance` → success JSON shape.
- Timeout / no clearance / queue full / proxy precedence.
- Regression: Turnstile createTask/getTaskResult tests remain green.

### miniflux (offline)

- Detector table tests (hard header rule).
- Cache: TTL/skew, proxyKey isolation, invalidate, max TTL cap.
- Client: httptest mock for `/v1/clearance`.
- Bypass orchestration: disabled path, miss→solve→retry, stale invalidate, singleflight coalescing.
- Handler/scraper wired with fake `ChallengeBypass` interface.

No live Cloudflare in CI.

## 11. Delivery split

| Slice | Content |
|-------|---------|
| P1 | camoufox `POST /v1/clearance` + offline tests |
| P2 | miniflux `cloudflare` package + config + unit tests |
| P3 | Wire CreateFeed / RefreshFeed / ScrapeWebsite + per-feed tri-state field |
| P4 | Optional UI + operator docs |

## 12. Security notes

- Intended for self-hosted miniflux operators fetching their own subscriptions.
- Solver default listen address localhost.
- Never commit secrets; proxy credentials only via config/env.
- Avoid logging full clearance values.
- Solving relies on normal browser automation (Camoufox), not hand-rolled CF protocol exploits.

## 13. Open points resolved during brainstorming

| Question | Resolution |
|----------|------------|
| Token vs clearance | Clearance cookies + UA |
| Expand service vs new service | Expand camoufox-turnstile |
| YesCaptcha type vs independent API | Independent `/v1/clearance` |
| Cache persistence | Memory only |
| Coverage | v1 feed + scraper |
| Cookie expiry | §7 full lifecycle |

## 14. Success criteria

1. With bypass enabled and solver healthy, a feed that returns CF challenge is refreshed successfully after at most one solve + one retry.
2. Scraper full-text on the same host reuses in-memory clearance when proxy matches and entry is unexpired.
3. Disabling global switch restores exact pre-feature behavior.
4. Turnstile token API for grok-register remains unchanged and tested.
5. All new tests pass offline.
