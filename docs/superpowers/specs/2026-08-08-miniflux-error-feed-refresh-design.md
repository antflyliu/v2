# Miniflux Error Feed Automatic Refresh Design

**Date**: 2026-08-08  
**Author**: Claude Code (design doc)  
**Status**: Draft (user-approved)

## 1. Context & Problem Statement

Miniflux currently has good built-in support for feed refresh through the worker pool. However, feeds that enter an error state (`parsing_error_msg IS NOT NULL`) often stay in that state for long periods, causing the user's RSS reader to remain broken.

The user needs a background process that:
- Periodically scans for feeds with persistent errors
- Triggers refresh on those feeds
- Handles high volume gracefully (no overwhelming the target sites)
- Integrates seamlessly with the existing dev startup script (`dev++.ps1`)

## 2. Requirements

- **Core Function**: Automatically refresh feeds that have persistent parsing errors
- **Filtering**: Only refresh feeds where `parsing_error_msg IS NOT NULL` and `parsing_error_count <= 5`
- **Concurrency & Safety**: 
  - Prefer serial execution when many error feeds exist
  - Add random backoff (3-5 seconds) between refreshes
- **Integration**: Must start and stop together with the main Miniflux process (via `dev++.ps1`)
- **Configuration**: Driven by environment variable (`ERROR_REFRESH_INTERVAL`)
- **Performance**: No HTTP API calls — direct internal call to `handler.RefreshFeed()`
- **Error Handling**: Never crash the main worker pool; continue to next feed even if one fails

## 3. Architecture

```
dev++.ps1
    │
    ▼
Miniflux (Go binary)
    ├── Main Worker Pool (normal feed polling)
    │
    └── Error Refresh Loop
            │
            ├── ticker (ERROR_REFRESH_INTERVAL)
            ├── serial refresh (or low-concurrency)
            ├── random 3-5s backoff between feeds
            ├── direct call: handler.RefreshFeed(store, userID, feedID, forceRefresh=true)
            └── shared shutdown channel
```

**Key Decisions**:
- **Direct internal call** (`handler.RefreshFeed`) instead of API — more efficient, better integration
- **Serial + random backoff** when many error feeds — safer than high concurrency
- **Error Refresh Loop** lives in the same worker package as the main pool
- **No UI changes** — system-level configuration only

## 4. Implementation Details

### 4.1 Configuration

Add to `internal/config/options.go`:

```go
"ERROR_REFRESH_INTERVAL": {
    parsedDuration: 30 * time.Minute,
    rawValue:       "30",
    valueType:      minuteType,
    validator: func(rawValue string) error {
        return validateGreaterOrEqualThan(rawValue, 0) // 0 = disabled
    },
},
```

Getter:
```go
func (c *configOptions) ErrorRefreshInterval() time.Duration { ... }
```

### 4.2 Core Logic (`internal/worker/error_refresh.go`)

```go
func StartErrorRefreshLoop(store *storage.Storage, shutdown <-chan struct{}, wg *sync.WaitGroup) {
    interval := config.Opts.ErrorRefreshInterval()
    if interval <= 0 {
        slog.Info("Error refresh disabled")
        return
    }

    // ... ticker logic ...

    refreshErrorFeeds(store)
}

func refreshErrorFeeds(store *storage.Storage) {
    // Use existing BatchBuilder with error limit
    jobs, _ := store.NewBatchBuilder().
        WithBatchSize(config.Opts.BatchSize()).
        WithErrorLimit(5).
        WithoutDisabledFeeds().
        FetchJobs()

    for _, job := range jobs {
        // Direct internal refresh
        if localizedError := feedHandler.RefreshFeed(store, job.UserID, job.FeedID, true); localizedError != nil {
            slog.Warn("Unable to refresh error feed", ...)
        }

        // Random backoff 3-5 seconds
        time.Sleep(3*time.Second + time.Duration(rand.Intn(2000))*time.Millisecond)
    }
}
```

### 4.3 Integration with Main Worker (`internal/worker/pool.go`)

```go
// In NewPool()
var errorWg sync.WaitGroup
StartErrorRefreshLoop(store, workerPool.shutdown, &errorWg)
```

### 4.4 dev++.ps1

Add to `.env.dev` (or pass via command line):

```powershell
ERROR_REFRESH_INTERVAL=30  # minutes, 0 = disable
```

## 5. Testing Strategy

- Unit tests for `refreshErrorFeeds` (mock storage)
- Integration test with real PostgreSQL
- Load test with 50+ error feeds (verify backoff works)
- Shutdown test (ensure loop stops cleanly when main process exits)

## 6. Trade-offs

| Option | Pros | Cons |
|--------|------|------|
| Serial + random backoff | Very safe, easy to understand | Slower when >30-50 error feeds |
| Low concurrency (5) | Still safe, faster overall | Slightly more risk of rate limiting |
| Direct internal call | Most efficient | Requires understanding of internal APIs |
| Environment variable only | Simple, consistent with existing config | No live UI toggle |

## 7. Rollback Plan

- Simply set `ERROR_REFRESH_INTERVAL=0` in `.env.dev`
- Or remove the new `error_refresh.go` and `StartErrorRefreshLoop` call

## 8. Next Steps

1. Implement the code changes
2. Update tests
3. Update documentation (README, CHANGELOG)
4. Verify with real error feeds

---

**Design Doc written.**  
Please review it and let me know if you want any changes before I proceed to implementation.