# Miniflux Error Feed Automatic Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement background automatic refresh for error feeds based on approved design doc.

**Architecture:** Direct internal `handler.RefreshFeed()` calls from error refresh loop (no API), serial execution with random 3-5s backoff, shared worker pool shutdown, environment-driven config.

**Tech Stack:** Go, existing `internal/worker`, `internal/reader/handler`, `internal/storage`, `internal/config`.

## Global Constraints

- Spec SSOT: `docs/superpowers/specs/2026-08-08-miniflux-error-feed-refresh-design.md` (user approved)
- Default `ERROR_REFRESH_INTERVAL = 0` (disabled) for test safety
- Serial refresh + random 3-5s backoff when many error feeds
- Never crash main worker pool
- Direct internal call to `handler.RefreshFeed()` (forceRefresh=true)
- No Settings page changes
- Clean shutdown with main pool

---

### Task 1: Config Changes

**Files:**
- Modify: `internal/config/options.go:560-580`

**Interfaces:**
- Consumes: `ERROR_REFRESH_INTERVAL`
- Produces: `config.Opts.ErrorRefreshInterval()`

- [ ] **Step 1: Add ERROR_REFRESH_INTERVAL to options map**

```go
"ERROR_REFRESH_INTERVAL": {
    parsedDuration: 0, // default disabled for tests
    rawValue:       "0",
    valueType:      minuteType,
    validator: func(rawValue string) error {
        return validateGreaterOrEqualThan(rawValue, 0)
    },
},
```

- [ ] **Step 2: Add getter**

```go
func (c *configOptions) ErrorRefreshInterval() time.Duration {
    return c.options["ERROR_REFRESH_INTERVAL"].parsedDuration
}
```

- [ ] **Step 3: Update tests if needed**

Run: `go test ./internal/config -run Test.*`

---

### Task 2: Create Error Refresh Module

**Files:**
- Create: `internal/worker/error_refresh.go`

**Interfaces:**
- Consumes: `config.Opts.ErrorRefreshInterval()`, `handler.RefreshFeed()`, `storage.Storage`
- Produces: `StartErrorRefreshLoop(store, shutdown, wg)`

- [ ] **Step 1: Write skeleton with default disabled**

```go
package worker

import (
    "log/slog"
    "time"

    "miniflux.app/v2/internal/config"
    "miniflux.app/v2/internal/reader/handler"
    "miniflux.app/v2/internal/storage"
)

func StartErrorRefreshLoop(store *storage.Storage, shutdown <-chan struct{}, wg *sync.WaitGroup) {
    interval := config.Opts.ErrorRefreshInterval()
    if interval <= 0 {
        slog.Info("Error refresh loop disabled (ERROR_REFRESH_INTERVAL=0)")
        return
    }

    // ... skeleton ...
}
```

- [ ] **Step 2: Add refreshErrorFeeds helper**

- [ ] **Step 3: Add random backoff**

- [ ] **Step 4: Run tests**

Run: `go test ./internal/worker -run ErrorRefresh`

---

### Task 3: Integrate with Main Worker Pool

**Files:**
- Modify: `internal/worker/pool.go:42-48`

**Interfaces:**
- Consumes: `StartErrorRefreshLoop`
- Produces: Error refresh loop starts automatically

- [ ] **Step 1: Add call to StartErrorRefreshLoop**

```go
var errorWg sync.WaitGroup
StartErrorRefreshLoop(store, workerPool.shutdown, &errorWg)
```

- [ ] **Step 2: Handle nil store in tests**

- [ ] **Step 3: Update pool_test.go**

- [ ] **Step 4: Run tests**

Run: `go test ./internal/worker -run TestShutdown`

---

### Task 4: dev++.ps1 Integration

**Files:**
- Modify: `.env.dev.example` or `dev++.ps1`

**Interfaces:**
- Consumes: `ERROR_REFRESH_INTERVAL=0` (default disabled)

- [ ] **Step 1: Add comment**

```powershell
# ERROR_REFRESH_INTERVAL=30  # 分钟，0=禁用
```

- [ ] **Step 2: Update README.md**

- [ ] **Step 3: Run build/test**

Run: `make test`

---

### Task 5: Complete & Review

**Files:**
- All changes above
- Test coverage
- Documentation

- [ ] **Step 1: Full code review**

- [ ] **Step 2: Run all tests**

Run: `make test`

- [ ] **Step 3: Commit**

```bash
git add internal/config/options.go internal/worker/error_refresh.go internal/worker/pool.go dev++.ps1
git commit -m "feat: add error feed automatic refresh"
```

---

**Plan complete and saved to `docs/superpowers/plans/2026-08-08-miniflux-error-feed-refresh-implementation-plan.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**