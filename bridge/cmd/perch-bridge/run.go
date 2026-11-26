package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/server"
	"github.com/th2zz/perch/bridge/internal/state"
)

func cmdRun(ctx context.Context, args []string) error {
	// The daemon keeps its own log unless a supervisor is already capturing our
	// output into that same file. See internal/state/logging.go for why: on WSL the
	// journal is not running, so "systemd will keep the output" is not true there.
	logOut := io.Writer(os.Stderr)
	if state.LogIfNotRedirected() {
		if dir, err := state.Dir(); err == nil {
			if f, err := state.OpenLog(dir, 1<<20); err == nil {
				// Deliberately not closed here: the file has to outlive this function,
				// because the message that says why the bridge died is printed *after*
				// it returns. A deferred Close closed it first, and the one line worth
				// keeping went to a dead handle. The process closing on exit is enough.
				logOut = io.MultiWriter(os.Stderr, f)
				errorSink = logOut
			} else {
				fmt.Fprintln(os.Stderr, "warning: cannot write a log file: "+err.Error())
			}
		}
	}

	c, err := newCoreWithLog(false, logOut)
	if err != nil {
		fatal(err)
	}
	// Through logOut rather than stdout: under systemd stdout is a socket into the
	// journal, which is exactly the thing that is missing on some platforms.
	printBanner(c, logOut)

	// A second copy must not silently fight the first. The lock is also what
	// makes `ensure-running` idempotent.
	lock, err := c.Store().Acquire()
	if err != nil {
		if errors.Is(err, state.ErrAlreadyRunning) {
			pid, _ := c.Store().RunningPID()
			fmt.Printf("perch bridge is already running (pid %d)\n", pid)
			return nil
		}
		return err
	}
	defer lock.Release()
	// From here on this process is the bridge, which is what the check-up must
	// report when it is asked from inside the daemon itself.
	c.MarkRunningAsDaemon()

	// Compatibility first: if the server speaks a protocol we do not
	// understand, say so plainly instead of rendering a wrong state (FR-015).
	if compat, err := herdr.CheckCompatWithTimeout(c.Client()); err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot reach herdr yet: "+err.Error())
	} else if !compat.OK {
		return errors.New(compat.Reason)
	}

	token, err := c.Store().EnsureToken()
	if err != nil {
		return err
	}
	srv, err := server.New(c, server.Options{
		Addr:    listenAddr(),
		Token:   token,
		Logger:  c.Logger(),
		Version: version,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); runPusher(ctx, c) }()

	return runUntilStopped(ctx, cancel, &wg, c.Run, srv.Run)
}

// runUntilStopped runs each component until the caller stops us or one of them fails.
//
// **The failure half is the whole point.** This used to wait on `<-ctx.Done()` alone:
// a component that died — most easily the HTTP server failing to bind its port — sent
// its error to a channel nobody read, and the process stayed up. Under systemd that
// looks like a healthy `active (running)` service while nothing is serving the phone,
// and the check-up agreed: it asks the pid file, which was still ours. Found by
// pointing the service at a port it may not bind (`PERCH_BRIDGE_ADDR=:1`) and watching
// it sit there, silent, for two minutes.
func runUntilStopped(
	ctx context.Context,
	cancel context.CancelFunc,
	wg *sync.WaitGroup,
	components ...func(context.Context) error,
) error {
	errCh := make(chan error, len(components))
	for _, run := range components {
		wg.Add(1)
		go func(run func(context.Context) error) {
			defer wg.Done()
			if err := run(ctx); err != nil {
				errCh <- err
			}
		}(run)
	}

	var failure error
	select {
	case <-ctx.Done():
	case failure = <-errCh:
	}
	// Either way, bring the rest down: a bridge is all of its parts or none of them.
	cancel()
	wg.Wait()
	if failure != nil && !errors.Is(failure, context.Canceled) {
		return failure
	}
	return nil
}

func printBanner(c *core.Core, out io.Writer) {
	// Errors here are not actionable: the worst case is a banner that did not appear.
	for _, line := range []string{
		fmt.Sprintf("perch-bridge %s", version),
		fmt.Sprintf("  herdr socket : %s", c.Client().Path()),
		fmt.Sprintf("  state dir    : %s", c.Store().Dir()),
		fmt.Sprintf("  listening    : %s", listenAddr()),
	} {
		_, _ = fmt.Fprintln(out, line)
	}
}

func listenAddr() string {
	if v := os.Getenv("PERCH_BRIDGE_ADDR"); v != "" {
		return v
	}
	return server.DefaultAddr
}

// runPusher forwards decision events to the push gateway, if one is configured.
// It never blocks the queue: a failed push is logged and the request stays
// visible in the app.
func runPusher(ctx context.Context, c *core.Core) {
	events, cancel := c.Watch()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type != "decision" || ev.Decision == nil {
				continue
			}
			cfg, err := c.PushConfig()
			if err != nil || cfg.GatewayURL == "" || cfg.DeviceToken == "" {
				continue
			}
			pusher := server.NewPusher(cfg.GatewayURL, cfg.GatewayToken)
			summary := server.SummaryFor(cfg.DeviceToken, c.HostLabel(), *ev.Decision)
			sendCtx, cancelSend := context.WithTimeout(ctx, 15*time.Second)
			err = pusher.Send(sendCtx, summary)
			cancelSend()
			if err != nil {
				c.Logger().Warn("push failed; the request is still visible in the app", "err", err)
				// A stale device token is worth telling the user about, but not
				// worth failing the run over.
				c.NotePushFailure(err)
			}
		}
	}
}
