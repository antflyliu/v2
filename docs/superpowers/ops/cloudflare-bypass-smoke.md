# Cloudflare bypass — local operator smoke checklist

Related scaling ops: `docs/superpowers/ops/scaling-b-runbook.md`

Local fork ops note for the clearance bypass (miniflux + camoufox-turnstile).  
Offline unit suites are CI-safe; **live Camoufox against real Cloudflare sites is a manual operator step only** (not automated here).

## Env vars (miniflux)

| Variable | Default | Meaning |
|----------|---------|---------|
| `CLOUDFLARE_BYPASS_ENABLED` | `0` | Master switch. Must be `1` for any solve/retry. |
| `CLOUDFLARE_BYPASS_URL` | empty | Base URL of camoufox-turnstile (e.g. `http://127.0.0.1:5072`). Empty disables bypass even if enabled. |
| `CLOUDFLARE_BYPASS_TIMEOUT` | `60` (seconds) | Solver HTTP timeout for `POST /v1/clearance`. |
| `CLOUDFLARE_BYPASS_CACHE_TTL` | `25` (minutes) | In-process clearance cache default TTL (skew 60s; hard cap 24h in code). |

Optional related:

- `FETCHER_ALLOW_PRIVATE_NETWORKS=1` — only if miniflux itself must hit private URLs; the solver is a separate HTTP client to `CLOUDFLARE_BYPASS_URL`.

## Start camoufox-turnstile

```bash
cd D:/WORKSPACE/ai-space/grok-register/services/camoufox-turnstile

# Offline / dry run (mock solver — no Camoufox binary required)
python -m app.main --config config.example.json

# Real browser solver (operator smoke against CF)
# copy config.example.json → config.json, set "solver": "camoufox"
python -m app.main --config config.json
```

Default listen: `http://127.0.0.1:5072`  
Clearance API: `POST /v1/clearance` (Turnstile YesCaptcha endpoints remain unchanged).

## Miniflux env for local smoke

```bash
export CLOUDFLARE_BYPASS_ENABLED=1
export CLOUDFLARE_BYPASS_URL=http://127.0.0.1:5072
export CLOUDFLARE_BYPASS_TIMEOUT=60
export CLOUDFLARE_BYPASS_CACHE_TTL=25
# optional if needed in your layout:
# export FETCHER_ALLOW_PRIVATE_NETWORKS=1
```

Then refresh or create a CF-blocked feed (same sticky proxy path as normal fetch).

## Expected behavior on a CF-blocked feed

1. First HTTP response is a Cloudflare challenge (detected via `cf-mitigated: challenge` / status+body rules).
2. Miniflux calls solver once: `POST {CLOUDFLARE_BYPASS_URL}/v1/clearance` with `websiteURL`, sticky `proxy`, optional `userAgent`, `timeoutSec`.
3. On success, retries the **same** feed/scrape request once with merged `cf_clearance` cookie + solver User-Agent.
4. Clearance is cached in-process by `host + proxyKey` for subsequent fetches until TTL/skew or reactive invalidation (retry still CF).

Design-level structured log fields (when present):

`event=challenge_detected` → `solve_ok` / `solve_err` → `retry_ok` / `retry_fail`  
(also: `cache_hit` / `cache_miss` / `cache_invalidate`)

Practical operator signals if event tags are sparse in a given build:

- Solver access log shows `POST /v1/clearance` with `mode=clearance` / phase logs.
- Feed leaves `error.http_cloudflare_challenge` after a successful solve+retry.
- Second refresh for same host+proxy should not re-solve until cache expiry (cache hit path).

## Sticky proxy

- Feed fetch and scraper resolve proxy **once** (`ResolveProxyURL`), then `WithLockedProxyURL` for all retries.
- Solver receives the same egress proxy string as the failing request.
- Cache key uses redacted proxy URL (`proxyURL.Redacted()`), not credentials in logs.
- Do **not** rotate proxy between challenge detect, solve, and business retry.

## Per-feed `cloudflare_bypass` tri-state

JSON field: `cloudflare_bypass` (`*bool`)

| Value | Behavior |
|-------|----------|
| omit / `null` | Follow global `CLOUDFLARE_BYPASS_ENABLED` + URL |
| `true` | Prefer enable for this feed (still requires global enable + non-empty URL) |
| `false` | Force disable for this feed |

v1 does **not** write clearance cookies into `feeds.cookie`.

## Offline verification (no live CF)

Service:

```bash
cd D:/WORKSPACE/ai-space/grok-register/services/camoufox-turnstile
python -m unittest discover -s tests -v
```

Miniflux:

```bash
cd D:/WORKSPACE/all-monitor-space/miniflux-v2
go test ./internal/reader/cloudflare/ ./internal/reader/fetcher/ ./internal/reader/handler/ ./internal/reader/scraper/ ./internal/reader/processor/ ./internal/config/ -count=1
go build -o miniflux.exe .
```

Known pre-existing skip on some Windows hosts: `TestParseAdminPasswordFileOptionWithEmptyFile` fails with `Access is denied` when creating `C:\WINDOWS\empty-password-*.txt` — unrelated to this feature; skip that test only if needed.

## Live smoke (manual only)

1. Start camoufox with `"solver": "camoufox"` and a usable sticky proxy if the target requires it.
2. Enable miniflux env vars above.
3. Refresh a known CF-blocked feed.
4. Confirm one clearance call + one successful content fetch; confirm cache avoids re-solve on immediate re-refresh.
5. Do not add live CF targets to automated CI.
