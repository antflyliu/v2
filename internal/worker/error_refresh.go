// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"miniflux.app/v2/internal/config"
	feedHandler "miniflux.app/v2/internal/reader/handler"
	"miniflux.app/v2/internal/storage"
)

// StartErrorRefreshLoop starts a background loop that periodically refreshes feeds
// with persistent parsing errors. Disabled when ERROR_REFRESH_INTERVAL <= 0.
// Shares the worker pool shutdown channel and wait group for clean exit.
func StartErrorRefreshLoop(store *storage.Storage, shutdown <-chan struct{}, wg *sync.WaitGroup) {
	interval := config.Opts.ErrorRefreshInterval()
	if interval <= 0 {
		slog.Info("Error refresh loop disabled (ERROR_REFRESH_INTERVAL=0)")
		return
	}

	if store == nil {
		slog.Warn("Error refresh loop not started: storage is nil")
		return
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		slog.Info("Starting error refresh loop", slog.Duration("interval", interval))

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-shutdown:
				slog.Info("Stopping error refresh loop")
				return
			case <-ticker.C:
				refreshErrorFeeds(store, shutdown)
			}
		}
	}()
}

func refreshErrorFeeds(store *storage.Storage, shutdown <-chan struct{}) {
	// Only retry feeds that currently have errors, and only while still under the
	// recoverable ceiling (parsing_error_count < 5 ⇒ count is 1..4).
	jobs, err := store.NewBatchBuilder().
		WithBatchSize(config.Opts.BatchSize()).
		WithErrorFeedsOnly().
		WithErrorLimit(5).
		WithoutDisabledFeeds().
		FetchJobs()
	if err != nil {
		slog.Error("Unable to fetch error feeds batch", slog.Any("error", err))
		return
	}

	if len(jobs) == 0 {
		return
	}

	slog.Info("Refreshing error feeds", slog.Int("count", len(jobs)))

	for i, job := range jobs {
		select {
		case <-shutdown:
			return
		default:
		}

		if localizedError := feedHandler.RefreshFeed(store, job.UserID, job.FeedID, true); localizedError != nil {
			slog.Warn("Unable to refresh error feed",
				slog.Int64("feed_id", job.FeedID),
				slog.Int64("user_id", job.UserID),
				slog.Any("error", localizedError.Error()),
			)
		}

		// Random backoff 3-5s between serial refreshes; skip after the last job.
		if i < len(jobs)-1 {
			if !sleepWithShutdown(shutdown, randomBackoff()) {
				return
			}
		}
	}
}

func randomBackoff() time.Duration {
	return 3*time.Second + time.Duration(rand.IntN(2000))*time.Millisecond
}

// sleepWithShutdown sleeps for d unless shutdown is closed first.
// Returns false if interrupted by shutdown.
func sleepWithShutdown(shutdown <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-shutdown:
		return false
	case <-timer.C:
		return true
	}
}
