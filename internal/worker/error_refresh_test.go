// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"sync"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
)

func TestStartErrorRefreshLoopDisabledByDefault(t *testing.T) {
	prev := config.Opts
	config.Opts = config.NewConfigOptions()
	t.Cleanup(func() { config.Opts = prev })

	if config.Opts.ErrorRefreshInterval() != 0 {
		t.Fatalf("expected default ERROR_REFRESH_INTERVAL=0, got %v", config.Opts.ErrorRefreshInterval())
	}

	var wg sync.WaitGroup
	shutdown := make(chan struct{})

	// Must return immediately without adding to the wait group.
	StartErrorRefreshLoop(nil, shutdown, &wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartErrorRefreshLoop blocked when disabled")
	}
}

func TestStartErrorRefreshLoopNilStoreWhenEnabled(t *testing.T) {
	prev := config.Opts
	t.Setenv("ERROR_REFRESH_INTERVAL", "30")
	opts, err := config.NewConfigParser().ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("unable to parse config: %v", err)
	}
	config.Opts = opts
	t.Cleanup(func() { config.Opts = prev })

	if config.Opts.ErrorRefreshInterval() <= 0 {
		t.Fatalf("expected ERROR_REFRESH_INTERVAL > 0, got %v", config.Opts.ErrorRefreshInterval())
	}

	var wg sync.WaitGroup
	shutdown := make(chan struct{})

	// Nil store must not start a goroutine or panic.
	StartErrorRefreshLoop(nil, shutdown, &wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StartErrorRefreshLoop blocked with nil store")
	}
}

func TestRandomBackoffRange(t *testing.T) {
	for range 50 {
		d := randomBackoff()
		if d < 3*time.Second || d >= 5*time.Second {
			t.Fatalf("randomBackoff out of range: %v", d)
		}
	}
}

func TestSleepWithShutdownInterrupted(t *testing.T) {
	shutdown := make(chan struct{})
	close(shutdown)

	start := time.Now()
	ok := sleepWithShutdown(shutdown, 5*time.Second)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("expected sleepWithShutdown to be interrupted")
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("sleepWithShutdown took too long after shutdown: %v", elapsed)
	}
}

func TestSleepWithShutdownCompletes(t *testing.T) {
	shutdown := make(chan struct{})
	start := time.Now()
	ok := sleepWithShutdown(shutdown, 20*time.Millisecond)
	elapsed := time.Since(start)

	if !ok {
		t.Fatal("expected sleepWithShutdown to complete")
	}
	if elapsed < 15*time.Millisecond {
		t.Fatalf("sleepWithShutdown returned too early: %v", elapsed)
	}
}
