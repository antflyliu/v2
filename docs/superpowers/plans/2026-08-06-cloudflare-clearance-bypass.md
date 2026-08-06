# Cloudflare Clearance Bypass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let self-hosted miniflux recover from Cloudflare Managed/JS Challenge by calling an extended camoufox-turnstile service that returns `cf_clearance` cookies, then retrying the same HTTP request with matching Cookie + User-Agent.

**Architecture:** camoufox-turnstile adds synchronous `POST /v1/clearance` (Camoufox opens URL, waits for `cf_clearance`). Miniflux adds `internal/reader/cloudflare` (detect, memory cache by host+proxy, HTTP client, orchestrate one solve + one retry) and wires it into feed create/refresh and scraper only. Existing Turnstile YesCaptcha API stays unchanged.

**Tech Stack:** Python 3 + stdlib HTTP server + Camoufox (service); Go 1.x miniflux fetcher/handler/scraper; offline unit tests only.

**Spec:** `docs/superpowers/specs/2026-08-06-cloudflare-clearance-bypass-design.md`

## Global Constraints

- No human browser interaction; headless Camoufox only inside camoufox-turnstile.
- Miniflux never embeds a browser.
- Independent `POST /v1/clearance` API; do not break `/createTask` + `/getTaskResult`.
- In-process cache only (no DB persistence of clearance cookies).
- v1 call sites: feed create/refresh + scraper full-text only.
- Sticky proxy: solve + retry must use the same egress proxy as the failing request.
- Per logical request: ≤1 solver call and ≤1 business HTTP retry after a successful solve (stale-cache path may invalidate then solve once).
- Cookie lifetime: `expiresAt` → cookie expires → default TTL 25m; apply 60s skew; hard cap 24h; reactive invalidation on still-CF.
- Tests must be offline (mock browser / httptest); no live Cloudflare.
- Small atomic commits; Chinese commit messages optional, conventional `feat:`/`test:`/`docs:` preferred for consistency with recent history.

## Two repositories

| Repo | Path |
|------|------|
| Service | `D:\WORKSPACE\ai-space\grok-register\services\camoufox-turnstile` |
| Miniflux | `D:\WORKSPACE\all-monitor-space\miniflux-v2` |

Implement **Phase A (service) fully before Phase B (miniflux client wiring)**. Tasks are ordered accordingly.

## File map

### camoufox-turnstile (create/modify)

| File | Role |
|------|------|
| `app/clearance.py` | **Create.** Clearance solve logic + mock solver protocol |
| `app/server.py` | **Modify.** Route `POST /v1/clearance`; inject clearance solver |
| `app/main.py` | **Modify.** Wire clearance solver into `ServiceState` if needed |
| `app/solver.py` | **Modify only if needed** for shared browser factory helpers — prefer not to break Turnstile |
| `tests/test_clearance_api.py` | **Create.** Offline API tests |
| `tests/test_clearance_solver.py` | **Create.** Mock browser clearance unit tests |
| `README.md` | **Modify.** Document `/v1/clearance` |
| `config.example.json` | **Modify.** Optional notes only |

### miniflux-v2 (create/modify)

| File | Role |
|------|------|
| `internal/reader/cloudflare/detector.go` | Challenge detection |
| `internal/reader/cloudflare/detector_test.go` | Detector tests |
| `internal/reader/cloudflare/cache.go` | Memory cache + singleflight |
| `internal/reader/cloudflare/cache_test.go` | Cache lifecycle tests |
| `internal/reader/cloudflare/client.go` | HTTP client for `/v1/clearance` |
| `internal/reader/cloudflare/client_test.go` | httptest tests |
| `internal/reader/cloudflare/bypass.go` | Orchestration |
| `internal/reader/cloudflare/bypass_test.go` | Orchestration tests |
| `internal/reader/cloudflare/cookies.go` | Cookie header merge helpers |
| `internal/reader/cloudflare/cookies_test.go` | Merge tests |
| `internal/reader/fetcher/request_builder.go` | Expose resolved proxy; optional execute-with-bypass helper |
| `internal/reader/fetcher/response_handler.go` | Delegate `isCloudflareChallenge` to detector (or keep thin wrapper) |
| `internal/config/options.go` | New env options + getters |
| `internal/config/options_parsing_test.go` | Parsing tests |
| `internal/model/feed.go` | `CloudflareBypass *bool` (tri-state) |
| `internal/database/migrations.go` | Nullable bool/int column for per-feed override |
| storage + API feed DTOs | Persist and expose field (minimal) |
| `internal/reader/handler/handler.go` | Wire bypass on create/refresh |
| `internal/reader/scraper/scraper.go` | Wire bypass on scrape |
| `internal/reader/processor/processor.go` | Pass policy into scraper path |

---

### Task 1: Service — clearance solver core (offline mock)

**Repo:** camoufox-turnstile

**Files:**
- Create: `app/clearance.py`
- Create: `tests/test_clearance_solver.py`

**Interfaces:**
- Produces:
  - `class ClearanceResult`: fields `ok: bool`, `cookies: list[dict]`, `user_agent: str`, `final_url: str`, `expires_at: int | None`, `error_code: str`, `error_description: str`
  - `class MockClearanceSolver` with `solve(website_url: str, proxy: str | None, user_agent: str = "", timeout_sec: float = 60) -> ClearanceResult`
  - `def proxy_precedence(task_proxy: str, config_proxy: str, allow_proxyless: bool) -> str` (or reuse `resolve_task_proxy` from `proxyutil`)

- [ ] **Step 1: Write failing unit tests for MockClearanceSolver**

```python
# tests/test_clearance_solver.py
from __future__ import annotations
import unittest
from app.clearance import MockClearanceSolver, ClearanceResult

class MockClearanceSolverTests(unittest.TestCase):
    def test_success_returns_cf_clearance(self):
        s = MockClearanceSolver()
        r = s.solve("https://www.hpcwire.com/feed", proxy="http://u:p@1.1.1.1:1")
        self.assertTrue(r.ok)
        names = [c["name"] for c in r.cookies]
        self.assertIn("cf_clearance", names)
        self.assertGreaterEqual(len(r.user_agent), 10)
        self.assertIsNotNone(r.expires_at)

    def test_empty_url_fails(self):
        s = MockClearanceSolver()
        r = s.solve("", proxy=None)
        self.assertFalse(r.ok)
        self.assertEqual(r.error_code, "ERROR_BAD_REQUEST")

    def test_force_no_clearance(self):
        s = MockClearanceSolver(force_error="ERROR_NO_CLEARANCE")
        r = s.solve("https://example.com", proxy=None)
        self.assertFalse(r.ok)
        self.assertEqual(r.error_code, "ERROR_NO_CLEARANCE")

if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run tests — expect fail (module missing)**

Run:
```bash
cd D:/WORKSPACE/ai-space/grok-register/services/camoufox-turnstile
python -m unittest tests.test_clearance_solver -v
```
Expected: `ModuleNotFoundError` or import error for `app.clearance`.

- [ ] **Step 3: Implement `app/clearance.py` (mock + result types + real solver skeleton)**

```python
"""Cloudflare Managed/JS Challenge → cf_clearance cookies."""
from __future__ import annotations

import logging
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Protocol
from urllib.parse import urlparse

log = logging.getLogger("camoufox-turnstile.clearance")

BrowserFactory = Callable[[str | None, bool], Any]  # (proxy, headless) -> CM yielding browser/context

@dataclass
class ClearanceResult:
    ok: bool
    cookies: list[dict[str, Any]] = field(default_factory=list)
    user_agent: str = ""
    final_url: str = ""
    expires_at: int | None = None
    error_code: str = ""
    error_description: str = ""

    def to_api_dict(self, website_url: str) -> dict[str, Any]:
        if not self.ok:
            return {
                "ok": False,
                "errorCode": self.error_code or "ERROR_SOLVER",
                "errorDescription": self.error_description or "",
            }
        return {
            "ok": True,
            "websiteURL": website_url,
            "finalURL": self.final_url or website_url,
            "userAgent": self.user_agent,
            "cookies": self.cookies,
            "expiresAt": self.expires_at,
        }


class ClearanceSolver(Protocol):
    def solve(
        self,
        website_url: str,
        proxy: str | None = None,
        user_agent: str = "",
        timeout_sec: float = 60,
    ) -> ClearanceResult: ...


class MockClearanceSolver:
    def __init__(
        self,
        force_error: str = "",
        user_agent: str = "Mozilla/5.0 MockClearance/1.0",
        ttl_sec: int = 1500,
    ) -> None:
        self.force_error = force_error
        self.user_agent = user_agent
        self.ttl_sec = ttl_sec
        self.last_url = ""
        self.last_proxy: str | None = None
        self.call_count = 0

    def solve(
        self,
        website_url: str,
        proxy: str | None = None,
        user_agent: str = "",
        timeout_sec: float = 60,
    ) -> ClearanceResult:
        self.call_count += 1
        self.last_url = website_url or ""
        self.last_proxy = proxy
        if not (website_url or "").strip():
            return ClearanceResult(
                ok=False,
                error_code="ERROR_BAD_REQUEST",
                error_description="websiteURL required",
            )
        if self.force_error:
            return ClearanceResult(
                ok=False,
                error_code=self.force_error,
                error_description="forced error",
            )
        host = urlparse(website_url).hostname or "example.com"
        exp = int(time.time()) + int(self.ttl_sec)
        ua = (user_agent or "").strip() or self.user_agent
        return ClearanceResult(
            ok=True,
            cookies=[
                {
                    "name": "cf_clearance",
                    "value": "mock-clearance-" + host,
                    "domain": "." + host.lstrip("."),
                    "path": "/",
                    "expires": exp,
                    "httpOnly": True,
                    "secure": True,
                    "sameSite": "None",
                }
            ],
            user_agent=ua,
            final_url=website_url,
            expires_at=exp,
        )


def cookie_expires_at(cookies: list[dict[str, Any]], fallback_ttl_sec: int = 1500) -> int:
    now = int(time.time())
    expiries: list[int] = []
    for c in cookies:
        if str(c.get("name") or "") != "cf_clearance":
            continue
        exp = c.get("expires")
        if exp is None:
            continue
        try:
            exp_i = int(exp)
        except (TypeError, ValueError):
            continue
        if exp_i > now:
            expiries.append(exp_i)
    if expiries:
        return min(expiries)
    return now + int(fallback_ttl_sec)


class CamoufoxClearanceSolver:
    """Real browser clearance. Uses injectable browser_factory for tests."""

    def __init__(
        self,
        *,
        headless: bool = True,
        solve_timeout_sec: float = 60,
        goto_timeout_ms: int = 45000,
        poll_interval_sec: float = 0.35,
        browser_factory: BrowserFactory | None = None,
        fallback_ttl_sec: int = 1500,
    ) -> None:
        self.headless = headless
        self.solve_timeout_sec = float(solve_timeout_sec)
        self.goto_timeout_ms = int(goto_timeout_ms)
        self.poll_interval_sec = float(poll_interval_sec)
        self.browser_factory = browser_factory
        self.fallback_ttl_sec = int(fallback_ttl_sec)
        self.last_user_agent = ""

    def solve(
        self,
        website_url: str,
        proxy: str | None = None,
        user_agent: str = "",
        timeout_sec: float = 60,
    ) -> ClearanceResult:
        url = (website_url or "").strip()
        if not url:
            return ClearanceResult(
                ok=False,
                error_code="ERROR_BAD_REQUEST",
                error_description="websiteURL required",
            )
        budget = float(timeout_sec or self.solve_timeout_sec)
        factory = self.browser_factory
        if factory is None:
            from app.solver import _default_camoufox_factory  # reuse existing helper

            factory = _default_camoufox_factory
        deadline = time.time() + budget
        try:
            with factory(proxy, self.headless) as browser:
                # Camoufox context manager yields browser-like object.
                page = browser.new_page()
                try:
                    if user_agent.strip():
                        try:
                            page.set_extra_http_headers({"User-Agent": user_agent.strip()})
                        except Exception:
                            pass
                    page.goto(url, timeout=self.goto_timeout_ms, wait_until="domcontentloaded")
                    while time.time() < deadline:
                        cookies = self._read_cookies(page, browser)
                        if any(c.get("name") == "cf_clearance" and c.get("value") for c in cookies):
                            ua = self._read_ua(page, user_agent)
                            self.last_user_agent = ua
                            exp = cookie_expires_at(cookies, self.fallback_ttl_sec)
                            final_url = ""
                            try:
                                final_url = str(page.url or url)
                            except Exception:
                                final_url = url
                            return ClearanceResult(
                                ok=True,
                                cookies=cookies,
                                user_agent=ua,
                                final_url=final_url,
                                expires_at=exp,
                            )
                        time.sleep(self.poll_interval_sec)
                    return ClearanceResult(
                        ok=False,
                        error_code="ERROR_NO_CLEARANCE",
                        error_description="cf_clearance not observed within timeout",
                    )
                finally:
                    try:
                        page.close()
                    except Exception:
                        pass
        except Exception as exc:  # noqa: BLE001
            log.exception("clearance solve failed url=%s", url)
            return ClearanceResult(
                ok=False,
                error_code="ERROR_SOLVER",
                error_description=str(exc)[:300],
            )

    def _read_ua(self, page: Any, fallback: str) -> str:
        try:
            ua = page.evaluate("() => navigator.userAgent")
            if ua and str(ua).strip():
                return str(ua).strip()
        except Exception:
            pass
        return (fallback or "").strip() or "Mozilla/5.0"

    def _read_cookies(self, page: Any, browser: Any) -> list[dict[str, Any]]:
        raw: list[Any] = []
        for obj in (page, browser, getattr(browser, "context", None)):
            if obj is None:
                continue
            for meth in ("cookies", "get_cookies"):
                fn = getattr(obj, meth, None)
                if callable(fn):
                    try:
                        got = fn()
                        if got:
                            raw = list(got)
                            break
                    except TypeError:
                        try:
                            got = fn(page.url)
                            if got:
                                raw = list(got)
                                break
                        except Exception:
                            pass
                    except Exception:
                        pass
            if raw:
                break
        out: list[dict[str, Any]] = []
        for c in raw:
            if not isinstance(c, dict):
                continue
            name = str(c.get("name") or "")
            if not name:
                continue
            item = {
                "name": name,
                "value": str(c.get("value") or ""),
                "domain": str(c.get("domain") or ""),
                "path": str(c.get("path") or "/"),
                "httpOnly": bool(c.get("httpOnly", c.get("http_only", False))),
                "secure": bool(c.get("secure", True)),
                "sameSite": str(c.get("sameSite") or c.get("same_site") or "None"),
            }
            if "expires" in c and c["expires"] not in (-1, None, 0, "-1"):
                try:
                    item["expires"] = int(float(c["expires"]))
                except (TypeError, ValueError):
                    pass
            out.append(item)
        return out
```

Note: Adjust `CamoufoxClearanceSolver` browser API to match actual Camoufox context used in `CamoufoxTurnstileSolver` (read that class when implementing — use the same factory pattern as `browser_pool` / `_default_camoufox_factory`). Mock path must not import Camoufox.

- [ ] **Step 4: Run unit tests**

```bash
python -m unittest tests.test_clearance_solver -v
```
Expected: PASS.

- [ ] **Step 5: Commit (service repo)**

```bash
cd D:/WORKSPACE/ai-space/grok-register/services/camoufox-turnstile
git add app/clearance.py tests/test_clearance_solver.py
git commit -m "feat(clearance): add mock clearance solver and result types"
```

---

### Task 2: Service — `POST /v1/clearance` HTTP API

**Files:**
- Modify: `app/server.py`
- Create: `tests/test_clearance_api.py`
- Modify: `app/main.py` (pass clearance solver into state if separate from turnstile solver)

**Interfaces:**
- Consumes: `ClearanceSolver.solve(...)`, `resolve_task_proxy` from `app/proxyutil`
- Produces: `POST /v1/clearance` JSON as in spec §5.1

- [ ] **Step 1: Write API tests**

```python
# tests/test_clearance_api.py
from __future__ import annotations
import json, threading, time, unittest, urllib.request
from app.server import ServiceState, create_server
from app.solver import MockSolver
from app.clearance import MockClearanceSolver

_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))

class ClearanceApiTests(unittest.TestCase):
    def setUp(self):
        self.clearance = MockClearanceSolver()
        self.state = ServiceState(
            solver=MockSolver(token="t" * 80),
            clearance_solver=self.clearance,
            config_proxy="http://cfg:1@8.8.8.8:8",
            allow_proxyless=True,
            max_browsers=1,
            queue_size=8,
        )
        self.httpd, _ = create_server("127.0.0.1", 0, state=self.state)
        self.port = self.httpd.server_address[1]
        self.base = "http://127.0.0.1:%s" % self.port
        self._thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self._thread.start()
        time.sleep(0.05)

    def tearDown(self):
        self.state.stop_workers()
        self.httpd.shutdown()
        self.httpd.server_close()

    def _post(self, path, body):
        data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            self.base + path, data=data,
            headers={"Content-Type": "application/json"}, method="POST",
        )
        with _OPENER.open(req, timeout=5) as resp:
            return json.loads(resp.read().decode("utf-8"))

    def test_clearance_success_task_proxy_wins(self):
        body = self._post("/v1/clearance", {
            "websiteURL": "https://www.hpcwire.com/x",
            "proxy": "http://task:1@1.1.1.1:1",
        })
        self.assertTrue(body.get("ok"))
        self.assertTrue(any(c["name"] == "cf_clearance" for c in body["cookies"]))
        self.assertIn("1.1.1.1", self.clearance.last_proxy or "")

    def test_clearance_bad_request(self):
        body = self._post("/v1/clearance", {"websiteURL": ""})
        self.assertFalse(body.get("ok"))
        self.assertEqual(body.get("errorCode"), "ERROR_BAD_REQUEST")

    def test_turnstile_still_works(self):
        created = self._post("/createTask", {
            "task": {
                "type": "TurnstileTask",
                "websiteURL": "https://example.com",
                "websiteKey": "k",
            }
        })
        self.assertEqual(created.get("errorId"), 0)
```

- [ ] **Step 2: Run — expect fail** (no route / no `clearance_solver` arg)

```bash
python -m unittest tests.test_clearance_api -v
```

- [ ] **Step 3: Extend `ServiceState` and handler in `app/server.py`**

Key changes (integrate carefully with existing file):

1. Add `clearance_solver` optional field on `ServiceState` (default `MockClearanceSolver()`).
2. In `do_POST`, if path ends with `/v1/clearance` or equals `/v1/clearance`, handle synchronously:

```python
def _clearance(self, payload: dict[str, Any]) -> None:
    website_url = str(payload.get("websiteURL") or payload.get("website_url") or "").strip()
    if not website_url:
        _json_response(self, 200, {
            "ok": False,
            "errorCode": "ERROR_BAD_REQUEST",
            "errorDescription": "websiteURL required",
        })
        return
    timeout_sec = float(payload.get("timeoutSec") or payload.get("timeout_sec") or state.task_timeout_sec)
    user_agent = str(payload.get("userAgent") or payload.get("user_agent") or "")
    # Build a synthetic task dict so resolve_task_proxy works
    task = {"proxy": payload.get("proxy") or ""}
    try:
        from app.proxyutil import ProxyRequiredError, resolve_task_proxy
        proxy = resolve_task_proxy(task, state.config_proxy, allow_proxyless=state.allow_proxyless)
    except ProxyRequiredError as exc:
        _json_response(self, 200, {
            "ok": False,
            "errorCode": exc.code,
            "errorDescription": str(exc),
        })
        return

    acquired = state._browser_slots.acquire(timeout=timeout_sec)
    if not acquired:
        _json_response(self, 200, {
            "ok": False,
            "errorCode": "ERROR_SLOT_TIMEOUT",
            "errorDescription": "browser slot timeout",
        })
        return
    try:
        solver = state.clearance_solver
        result = solver.solve(
            website_url,
            proxy=proxy or None,
            user_agent=user_agent,
            timeout_sec=timeout_sec,
        )
        _json_response(self, 200, result.to_api_dict(website_url))
    except Exception as exc:  # noqa: BLE001
        log.exception("clearance endpoint failed")
        _json_response(self, 200, {
            "ok": False,
            "errorCode": "ERROR_SOLVER",
            "errorDescription": str(exc)[:300],
        })
    finally:
        state._browser_slots.release()
```

3. Update `ServiceState.__init__` signature with `clearance_solver=None`.
4. Update `app/main.py` `build_state` to construct `CamoufoxClearanceSolver` when `solver==camoufox`, else `MockClearanceSolver`.

- [ ] **Step 4: Run all service tests**

```bash
python -m unittest discover -s tests -v
```
Expected: all PASS (including existing Turnstile tests).

- [ ] **Step 5: Update README with `/v1/clearance` section** (request/response examples from spec).

- [ ] **Step 6: Commit**

```bash
git add app/server.py app/main.py app/clearance.py tests/test_clearance_api.py README.md
git commit -m "feat(clearance): add POST /v1/clearance synchronous API"
```

---

### Task 3: Miniflux — detector + cookie merge helpers

**Repo:** miniflux-v2

**Files:**
- Create: `internal/reader/cloudflare/detector.go`
- Create: `internal/reader/cloudflare/detector_test.go`
- Create: `internal/reader/cloudflare/cookies.go`
- Create: `internal/reader/cloudflare/cookies_test.go`

- [ ] **Step 1: Failing detector tests**

```go
package cloudflare_test

import (
	"net/http"
	"testing"

	"miniflux.app/v2/internal/reader/cloudflare"
)

func TestIsChallenge(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header http.Header
		want   bool
	}{
		{
			name:   "hard CF challenge",
			status: 403,
			header: http.Header{
				"Cf-Mitigated": []string{"challenge"},
				"Content-Type": []string{"text/html; charset=utf-8"},
			},
			want: true,
		},
		{
			name:   "plain 403",
			status: 403,
			header: http.Header{"Content-Type": []string{"text/html"}},
			want:   false,
		},
		{
			name:   "200 html",
			status: 200,
			header: http.Header{
				"Cf-Mitigated": []string{"challenge"},
				"Content-Type": []string{"text/html"},
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: tc.header}
			if got := cloudflare.IsChallenge(resp); got != tc.want {
				t.Fatalf("IsChallenge()=%v want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Implement detector**

```go
// internal/reader/cloudflare/detector.go
package cloudflare

import (
	"net/http"
	"strings"
)

// IsChallenge reports whether resp looks like a Cloudflare bot interstitial.
// Hard rule only (v1): 403 + cf-mitigated:challenge + text/html.
func IsChallenge(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	if !strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return false
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.HasPrefix(ct, "text/html")
}
```

- [ ] **Step 3: Cookie merge tests + implementation**

```go
// cookies.go
package cloudflare

import (
	"fmt"
	"strings"
)

type Cookie struct {
	Name  string
	Value string
}

func ParseCookieHeader(h string) []Cookie {
	// split on ';', then name=value; trim spaces
}

func FormatCookieHeader(cookies []Cookie) string {
	// name=value; name2=value2
}

// MergeCookies: existing feed cookies + solver cookies; same name → solver wins.
func MergeCookies(existingHeader string, solver []Cookie) string {
	m := map[string]string{}
	order := []string{}
	for _, c := range ParseCookieHeader(existingHeader) {
		if _, ok := m[c.Name]; !ok {
			order = append(order, c.Name)
		}
		m[c.Name] = c.Value
	}
	for _, c := range solver {
		if c.Name == "" {
			continue
		}
		if _, ok := m[c.Name]; !ok {
			order = append(order, c.Name)
		}
		m[c.Name] = c.Value
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		parts = append(parts, fmt.Sprintf("%s=%s", n, m[n]))
	}
	return strings.Join(parts, "; ")
}

func CookiesFromAPI(items []map[string]any) []Cookie // or typed struct from client
```

Prefer typed structs in client (Task 5) and convert here.

- [ ] **Step 4: Run tests**

```bash
cd D:/WORKSPACE/all-monitor-space/miniflux-v2
go test ./internal/reader/cloudflare/ -count=1
```
Expected: PASS for detector + cookies.

- [ ] **Step 5: Commit**

```bash
git add internal/reader/cloudflare/
git commit -m "feat(cloudflare): add challenge detector and cookie merge helpers"
```

---

### Task 4: Miniflux — in-process cache + singleflight

**Files:**
- Create: `internal/reader/cloudflare/cache.go`
- Create: `internal/reader/cloudflare/cache_test.go`

**Interfaces:**
- Produces:
  - `type CacheEntry struct { CookieHeader, UserAgent string; ExpiresAt time.Time }`
  - `func CacheKey(host, proxyKey string) string`
  - `type Cache struct` with `Get`, `Set`, `Invalidate`, `GetOrSolve(key, solve func() (CacheEntry, error)) (CacheEntry, error)`

- [ ] **Step 1: Write cache tests covering §7 lifecycle**

Cases:
1. Set then Get before expiry → hit  
2. Set with ExpiresAt in past → Get miss  
3. Skew: entry expires 30s from now with 60s skew → treat as miss on Set or Get (implement skew at Set)  
4. Different proxyKey → isolated  
5. Invalidate → miss  
6. Max TTL cap 24h  
7. `GetOrSolve` singleflight: two concurrent callers, solve runs once  

```go
func TestCacheSkewAndInvalidate(t *testing.T) {
	c := cloudflare.NewCache(cloudflare.CacheOptions{
		DefaultTTL: 25 * time.Minute,
		Skew:       60 * time.Second,
		MaxTTL:     24 * time.Hour,
	})
	host := "www.example.com"
	key := cloudflare.CacheKey(host, "direct")
	c.Set(key, cloudflare.CacheEntry{
		CookieHeader: "cf_clearance=abc",
		UserAgent:    "UA",
		ExpiresAt:    time.Now().Add(30 * time.Second), // within skew → should not be stored as usable long
	})
	// After Set applies skew, either entry not stored or already expired for Get
	if _, ok := c.Get(key); ok {
		// If implementation stores but Get applies skew, still miss:
		t.Fatal("expected miss when remaining life < skew")
	}
}
```

Implement Set as: `effective = expiresAt.Add(-skew)`; if `effective.Before(now)` do not store (or store and Get misses). Hot cookies for immediate retry are held by orchestrator, not cache.

- [ ] **Step 2: Implement cache**

Use `sync.Mutex` + `golang.org/x/sync/singleflight` (check if module already in `go.mod`; if not, use a small local singleflight map of `chan struct{}` to avoid new deps if preferred). Miniflux may already depend on x/sync — verify with `go.mod`. Prefer stdlib-only local singleflight if adding deps is undesirable:

```go
// minimal singleflight
type group struct {
	mu sync.Mutex
	m  map[string]*call
}
type call struct {
	wg  sync.WaitGroup
	val CacheEntry
	err error
}
```

- [ ] **Step 3: `go test ./internal/reader/cloudflare/ -count=1` → PASS**

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(cloudflare): add clearance memory cache with skew and singleflight"
```

---

### Task 5: Miniflux — clearance HTTP client

**Files:**
- Create: `internal/reader/cloudflare/client.go`
- Create: `internal/reader/cloudflare/client_test.go`

**Interfaces:**
- Produces:
```go
type SolveRequest struct {
	WebsiteURL string
	Proxy      string
	Timeout    time.Duration
	UserAgent  string
}
type SolveResponse struct {
	OK          bool
	Cookies     []Cookie
	UserAgent   string
	FinalURL    string
	ExpiresAt   time.Time // zero if absent
	ErrorCode   string
	ErrorDesc   string
}
type Client struct { BaseURL string; HTTPClient *http.Client }
func (c *Client) Solve(ctx context.Context, req SolveRequest) (*SolveResponse, error)
```

- [ ] **Step 1: httptest tests** — success JSON, `ok:false` business error, server 500, context timeout.

- [ ] **Step 2: Implement client** posting to `strings.TrimRight(base,"/") + "/v1/clearance"` with JSON body field names matching service (`websiteURL`, `proxy`, `timeoutSec`, `userAgent`).

- [ ] **Step 3: Allow private network to localhost** — client used only for solver; create `http.Client` with normal dial (solver is 127.0.0.1). Do not use fetcher private-network block for solver calls. Document that `CLOUDFLARE_BYPASS_URL` is trusted local service.

- [ ] **Step 4: Test + commit**

```bash
go test ./internal/reader/cloudflare/ -count=1
git add internal/reader/cloudflare/
git commit -m "feat(cloudflare): add /v1/clearance HTTP client"
```

---

### Task 6: Miniflux — config options

**Files:**
- Modify: `internal/config/options.go` (add keys in `NewConfigOptions` map + getters)
- Modify: `internal/config/options_parsing_test.go`

**Options (exact keys):**

| Key | Type | Default |
|-----|------|---------|
| `CLOUDFLARE_BYPASS_URL` | string | `""` |
| `CLOUDFLARE_BYPASS_ENABLED` | bool | `false` |
| `CLOUDFLARE_BYPASS_TIMEOUT` | duration seconds | `60` (use existing second/int pattern used by `HTTP_CLIENT_TIMEOUT`) |
| `CLOUDFLARE_BYPASS_CACHE_TTL` | duration | `25` minutes — if only second/int types exist, store as seconds `1500` or minutes type like other day/hour keys; pick the closest existing `valueType` (prefer seconds int → `time.Duration`) |

Read how `HTTP_CLIENT_TIMEOUT` is declared in `options.go` and mirror it.

Getters:

```go
func (c *configOptions) CloudflareBypassURL() string
func (c *configOptions) CloudflareBypassEnabled() bool
func (c *configOptions) CloudflareBypassTimeout() time.Duration
func (c *configOptions) CloudflareBypassCacheTTL() time.Duration
```

- [ ] **Step 1: Add parsing tests** (default off; enable with URL + `1`)

- [ ] **Step 2: Implement options + getters**

- [ ] **Step 3: `go test ./internal/config/ -count=1` → PASS**

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(config): add CLOUDFLARE_BYPASS_* options"
```

---

### Task 7: Miniflux — bypass orchestrator

**Files:**
- Create: `internal/reader/cloudflare/bypass.go`
- Create: `internal/reader/cloudflare/bypass_test.go`

**Interfaces:**

```go
type Policy struct {
	// FeedOverride: nil = follow global; &true / &false = force
	FeedOverride *bool
}

type Bypass struct {
	client *Client
	cache  *Cache
	// enabled from config
	url     string
	enabled bool
	timeout time.Duration
}

func NewBypassFromConfig() *Bypass // reads config.Opts; nil-safe if Opts nil in tests

func (b *Bypass) Enabled(p Policy) bool

// ResolvedProxy is the proxy URL string used for the business request, or "".
func (b *Bypass) Execute(
	ctx context.Context,
	requestURL string,
	resolvedProxy string,
	policy Policy,
	build func(cookie, ua string) (*http.Response, error),
) (resp *http.Response, err error)
```

Orchestration algorithm (must match spec §6.4 + §7):

1. If `!Enabled(policy)` → `return build("", "")`  // caller still sets feed cookie/ua outside; simpler: `build` always receives optional override cookie/ua empty meaning “use builder defaults”
2. Better API used by wiring:

```go
// DoRequest applies cache, performs request via fn, on CF solves once and retries once.
func (b *Bypass) DoRequest(
	ctx context.Context,
	requestURL string,
	proxyKey string,      // "direct" or redacted proxy URL
	proxyForSolver string,// actual proxy string for solver (may include creds)
	policy Policy,
	existingCookie string,
	existingUA string,
	do func(cookie, userAgent string) (*http.Response, error),
) (*http.Response, error)
```

Logic:
```
cookie, ua := existingCookie, existingUA
appliedCache := false
if b.Enabled(policy) {
  if ent, ok := b.cache.Get(CacheKey(host, proxyKey)); ok {
    cookie = MergeCookies(existingCookie, ParseCookieHeader(ent.CookieHeader))
    if ent.UserAgent != "" { ua = ent.UserAgent }
    appliedCache = true
  }
}
resp, err := do(cookie, ua)
if err != nil || !IsChallenge(resp) || !b.Enabled(policy) {
  return resp, err
}
// CF path
if appliedCache {
  b.cache.Invalidate(key)
}
entry, err := b.cache.GetOrSolve(key, func() (CacheEntry, error) {
  sol, err := b.client.Solve(ctx, SolveRequest{WebsiteURL: requestURL, Proxy: proxyForSolver, Timeout: b.timeout})
  ...
})
// on solve fail: return original resp (caller LocalizedError → CF)
// on success: merge cookies, set ua, do() once more
// if still CF: Invalidate; return second resp
// if OK: cache already set inside GetOrSolve; return
```

- [ ] **Step 1: Orchestration tests with fake `do` and fake client** (interface for client)

Define:
```go
type Solver interface {
	Solve(ctx context.Context, req SolveRequest) (*SolveResponse, error)
}
```
so tests inject fake solver without httptest.

Cases:
- disabled → do called once, solver never  
- CF then solve ok then 200 → do twice, solver once  
- cache hit → solver zero, do once if 200  
- cache hit still CF → invalidate, solver once, do twice total after first CF  
- solve error → do once, return first CF resp  

- [ ] **Step 2: Implement bypass.go**

- [ ] **Step 3: Test + commit**

```bash
go test ./internal/reader/cloudflare/ -count=1
git commit -am "feat(cloudflare): add bypass orchestrator"
```

---

### Task 8: Miniflux — expose resolved proxy from RequestBuilder

**Files:**
- Modify: `internal/reader/fetcher/request_builder.go`
- Modify: `internal/reader/fetcher/request_builder_test.go`

**Problem:** Proxy is chosen inside `ExecuteRequest`; bypass needs the same value for solver + cache key and must not re-call rotator on retry.

**Solution:**

1. Extract method:
```go
func (r *RequestBuilder) ResolveProxyURL() (*url.URL, error)
```
Same switch as current `ExecuteRequest` (feed proxy → app proxy → rotator).

2. Add:
```go
func (r *RequestBuilder) WithLockedProxyURL(u *url.URL) *RequestBuilder
```
When set, `ExecuteRequest` uses it and **skips** rotator.

3. Optionally store last resolved proxy on builder for logging.

- [ ] **Step 1: Tests** — with feed proxy set, `ResolveProxyURL` returns it; with locked proxy, Execute does not advance rotator (use a fake rotator if available, or unit-test Resolve only).

- [ ] **Step 2: Implement extraction without behavior change for existing callers**

- [ ] **Step 3: `go test ./internal/reader/fetcher/ -count=1` → PASS

- [ ] **Step 4: Commit**

```bash
git commit -am "refactor(fetcher): expose ResolveProxyURL and locked proxy for sticky retries"
```

---

### Task 9: Miniflux — per-feed tri-state field + migration

**Files:**
- Modify: `internal/model/feed.go` — add `CloudflareBypass *bool \`json:"cloudflare_bypass"\``
- Modify: `internal/database/migrations.go` — append migration
- Modify storage feed read/write SQL (search `cookie` column usage in `internal/storage`)
- Modify API feed create/update payloads if present (`internal/api`, `internal/model` request types)
- UI optional in v1 — **skip UI** unless trivial; API/DB enough per spec P3

**Migration SQL:**

```sql
ALTER TABLE feeds ADD COLUMN cloudflare_bypass boolean;
-- NULL = follow global; true/false = override
```

- [ ] **Step 1: Add migration function at end of migrations slice (idempotent if project pattern supports)**

If migrations are append-only functions, add new func; do not edit old ones.

- [ ] **Step 2: Storage scan/update include column** — grep `f.cookie` / `user_agent` in storage feed files and mirror.

- [ ] **Step 3: Model + API request structs** for create/update feed.

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(feed): add cloudflare_bypass tri-state column"
```

Note: schema change requires user confirmation in some workflows — already approved in design.

---

### Task 10: Miniflux — wire CreateFeed / RefreshFeed

**Files:**
- Modify: `internal/reader/handler/handler.go`

**Pattern for RefreshFeed (same idea for CreateFeed):**

```go
requestBuilder := fetcher.NewRequestBuilder(). /* existing chain */

proxyURL, proxyErr := requestBuilder.ResolveProxyURL()
proxyKey := "direct"
proxyForSolver := ""
if proxyErr == nil && proxyURL != nil {
	proxyKey = proxyURL.Redacted()
	proxyForSolver = proxyURL.String()
	requestBuilder = requestBuilder.WithLockedProxyURL(proxyURL)
}

bypass := cloudflare.NewBypassFromConfig()
policy := cloudflare.Policy{FeedOverride: originalFeed.CloudflareBypass}

var httpResp *http.Response
var clientErr error
if bypass.Enabled(policy) {
	httpResp, clientErr = bypass.DoRequest(
		context.Background(),
		originalFeed.FeedURL,
		proxyKey,
		proxyForSolver,
		policy,
		originalFeed.Cookie,
		/* effective UA string */,
		func(cookie, ua string) (*http.Response, error) {
			b := requestBuilder.Clone().
				WithCookie(cookie).
				WithUserAgent(ua, config.Opts.HTTPClientUserAgent())
			return b.ExecuteRequest(originalFeed.FeedURL)
		},
	)
} else {
	httpResp, clientErr = requestBuilder.ExecuteRequest(originalFeed.FeedURL)
}
responseHandler := fetcher.NewResponseHandler(httpResp, clientErr)
```

Ensure `Clone` exists (already does). When bypass disabled, behavior identical to today.

- [ ] **Step 1: Implement wiring carefully; keep error handling after responseHandler unchanged**

- [ ] **Step 2: Compile**

```bash
go test ./internal/reader/handler/ -count=1
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git commit -am "feat(handler): wire Cloudflare bypass into feed create/refresh"
```

---

### Task 11: Miniflux — wire scraper path

**Files:**
- Modify: `internal/reader/scraper/scraper.go`
- Modify: `internal/reader/processor/processor.go` (pass feed override + ensure requestBuilder uses locked proxy)

**Approach:** Extend `ScrapeWebsite` signature minimally:

```go
func ScrapeWebsite(requestBuilder *fetcher.RequestBuilder, pageURL, rules string, policy cloudflare.Policy) (string, string, error)
```

Or attach policy via optional parameter struct to avoid breaking many call sites — check all `ScrapeWebsite` callers with grep.

Inside scraper: same ResolveProxyURL + DoRequest pattern as handler; on non-enabled, current code path.

- [ ] **Step 1: Grep callers and update**

- [ ] **Step 2: processor passes `cloudflare.Policy{FeedOverride: feed.CloudflareBypass}`**

- [ ] **Step 3: `go test ./internal/reader/scraper/ ./internal/reader/processor/ ./internal/reader/handler/ -count=1`**

- [ ] **Step 4: Commit**

```bash
git commit -am "feat(scraper): wire Cloudflare bypass for full-text fetch"
```

---

### Task 12: ResponseHandler alignment + package docs

**Files:**
- Modify: `internal/reader/fetcher/response_handler.go` — implement `isCloudflareChallenge` by calling `cloudflare.IsChallenge(r.httpResponse)` to avoid dual logic.
- Watch import cycles: `cloudflare` must not import `fetcher`. Detector takes `*http.Response` only — OK.

- [ ] **Step 1: Change method body to delegate**

- [ ] **Step 2: Existing `response_handler_test.go` CF tests still pass (add if missing)**

- [ ] **Step 3: Commit**

```bash
git commit -am "refactor(fetcher): delegate CF challenge detection to cloudflare package"
```

---

### Task 13: Integration smoke (offline) + operator notes

**Files:**
- Modify: miniflux `docs/superpowers/specs/...` only if needed — prefer short ops note in plan completion comment or `README` snippet for local fork (do not invent large upstream README changes unless user wants).

- [ ] **Step 1: Run full offline suites**

Service:
```bash
cd D:/WORKSPACE/ai-space/grok-register/services/camoufox-turnstile
python -m unittest discover -s tests -v
```

Miniflux:
```bash
cd D:/WORKSPACE/all-monitor-space/miniflux-v2
go test ./internal/reader/cloudflare/ ./internal/reader/fetcher/ ./internal/reader/handler/ ./internal/reader/scraper/ ./internal/config/ -count=1
go build -o miniflux.exe .
```

- [ ] **Step 2: Manual local smoke (operator, not CI)**

1. Start camoufox with mock or camoufox solver:  
   `python -m app.main --config config.json`
2. Run miniflux with:
   ```
   CLOUDFLARE_BYPASS_ENABLED=1
   CLOUDFLARE_BYPASS_URL=http://127.0.0.1:5072
   FETCHER_ALLOW_PRIVATE_NETWORKS=1   # if solver checks ever needed; solver is separate client
   ```
3. Refresh a CF-blocked feed; expect logs `event=challenge_detected` then `solve_ok` / `retry_ok`.

- [ ] **Step 3: Final commit if only docs/test fixes remain**

---

## Spec coverage checklist

| Spec section | Task(s) |
|--------------|---------|
| §5 `/v1/clearance` API | 1–2 |
| §5 Turnstile unchanged | 2 regression tests |
| §6 package layout | 3–7 |
| §6 config + per-feed | 6, 9 |
| §6 detect hard rule | 3, 12 |
| §6 orchestration | 7, 10, 11 |
| §6 sticky proxy | 8, 10, 11 |
| §7 cookie lifecycle | 4, 7 |
| §8 errors | 7 |
| §10 offline tests | all tasks |
| §11 P1–P3 delivery | Tasks 1–2 = P1; 3–7 = P2; 9–11 = P3 |

## Placeholder / consistency self-review

- No TBD steps; dual-repo paths absolute.
- API field names aligned: `websiteURL`, `userAgent`, `timeoutSec`, `expiresAt`, `errorCode`.
- Cache skew 60s, default TTL 25m, max 24h match spec.
- `DoRequest` / `Solve` naming consistent across tasks 5–7.
- Import cycle avoided: cloudflare ← fetcher/handler/scraper; cloudflare does not import fetcher.

## Risk notes for implementers

1. **Camoufox cookie API** may differ slightly — Task 1 real solver must be validated against existing `CamoufoxTurnstileSolver` browser usage; mock tests do not need Camoufox installed.
2. **Rotator stickiness** is easy to get wrong — always `ResolveProxyURL` once, `WithLockedProxyURL`, then all retries.
3. **Do not write clearance into `feeds.cookie`** in v1.
4. **JSON `cloudflare_bypass` null** vs omit — use pointer `*bool` for tri-state.
