package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestAComponentFailureStopsTheBridge.
//
// The bug this exists for: the daemon used to wait on `<-ctx.Done()` alone, so a
// component that died — most easily the HTTP server failing to bind — left a process
// that stayed up, served nothing, and looked healthy to both systemd and our own
// check-up (which asks the pid file, and the pid file was still ours). The phone
// simply never heard from the computer again, with nothing anywhere saying why.
//
// The timeout is the assertion: with the old code this test never returns.
func TestAComponentFailureStopsTheBridge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boom := errors.New("listen on :1: bind: permission denied")
	dead := make(chan struct{})
	var deadOnce sync.Once

	var wg sync.WaitGroup
	done := make(chan error, 1)
	go func() {
		done <- runUntilStopped(ctx, cancel, &wg,
			func(context.Context) error { return boom },
			func(runCtx context.Context) error {
				// A healthy component: it stays until it is told to stop, and it must
				// actually be stopped — a bridge that leaves half of itself running
				// would hold the port, the state lock and the herdr subscription.
				<-runCtx.Done()
				deadOnce.Do(func() { close(dead) })
				return nil
			},
		)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Errorf("returned %v, want the component's failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a failed component did not stop the bridge; the process would stay up serving nothing")
	}

	select {
	case <-dead:
	case <-time.After(2 * time.Second):
		t.Error("the healthy component was not stopped")
	}
}

func TestStoppingTheCallerStopsTheComponents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	stopped := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runUntilStopped(ctx, cancel, &wg, func(runCtx context.Context) error {
			<-runCtx.Done()
			close(stopped)
			return nil
		})
	}()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a clean stop reported %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not return")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Error("the component was not asked to stop")
	}
}

// TestAContextCancelIsNotReportedAsAFailure: a shutdown is not an error, and
// reporting one would make systemd treat every restart as a crash.
func TestAContextCancelIsNotReportedAsAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	done := make(chan error, 1)
	go func() {
		done <- runUntilStopped(ctx, cancel, &wg, func(runCtx context.Context) error {
			<-runCtx.Done()
			return context.Canceled
		})
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a cancelled component was reported as a failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not return")
	}
}
