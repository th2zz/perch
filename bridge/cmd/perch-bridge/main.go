// Command bridge is the Perch host-side bridge.
//
// It is a single binary with two faces:
//
//   - `perch-bridge run` is the long-lived daemon. systemd (Linux/WSL) or
//     launchd (macOS) supervises it; herdr's plugin startup hook only calls
//     `ensure-running`, because a herdr startup hook is a one-shot command and
//     not a supervisor (lab/bridge-probe, 08.5).
//   - every other subcommand is a one-shot: install, check up, print status,
//     accept an event from herdr. They all run in milliseconds and exit.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/th2zz/perch/bridge/internal/core"
)

// version is stamped at build time.
var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "run":
		err = cmdRun(ctx, args)
	case "status":
		err = cmdStatus(ctx, args)
	case "doctor":
		err = cmdDoctor(ctx, args)
	case "ensure-running":
		err = cmdEnsureRunning(ctx, args)
	case "logs":
		err = cmdLogs(ctx, args)
	case "on-event":
		err = cmdOnEvent(ctx, args)
	case "pair":
		err = cmdPair(ctx, args)
	case "snapshot":
		err = cmdSnapshot(ctx, args)
	case "fingerprint":
		err = cmdFingerprint(ctx, args)
	case "install-service":
		err = cmdInstallService(ctx, args)
	case "render-unit":
		err = cmdRenderUnit(args)
	case "uninstall-service":
		err = cmdUninstallService(ctx, args)
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		if errors.Is(err, errDoctorFailed) {
			os.Exit(1)
		}
		_, _ = fmt.Fprintln(errorSink, "error: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `perch-bridge — Perch host-side bridge

Usage: perch-bridge <command>

  run                Run the bridge in the foreground (systemd/launchd calls this)
  ensure-running     Start the bridge if it is not already running (plugin hook)
  on-event           Accept one herdr event (plugin hook; writes and exits)
  status             Show what the bridge currently sees
  doctor             Check the seven things that must work, with fixes
  pair               Create a pairing token for the phone
  snapshot           Print the read-only mirror as JSON
  fingerprint        Record or compare what Perch must not change
  logs               Show the bridge's recent activity
  install-service    Install the systemd/launchd unit that keeps the bridge alive
  render-unit        Print the unit install-service would write (audit it first)
  uninstall-service  Remove that unit and stop the bridge
  version            Print the version
`)
}

// newCore builds the runtime with logging that is quiet enough for a hook
// process and useful in a daemon.
func newCore(disableSubscriptions bool) (*core.Core, error) {
	return newCoreWithLog(disableSubscriptions, os.Stderr)
}

// newCoreWithLog is newCore with somewhere else to write.
//
// The daemon passes a writer that reaches both the terminal and its own log file: the
// terminal because someone may be watching, the file because on some platforms nobody
// else is keeping the output (see internal/state/logging.go).
func newCoreWithLog(disableSubscriptions bool, out io.Writer) (*core.Core, error) {
	level := slog.LevelInfo
	if os.Getenv("PERCH_DEBUG") != "" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level}))
	return core.New(core.Options{
		Logger:               logger,
		DisableSubscriptions: disableSubscriptions,
	})
}

func mustCore(disableSubscriptions bool) *core.Core {
	c, err := newCore(disableSubscriptions)
	if err != nil {
		fatal(err)
	}
	return c
}

// errorSink is where a fatal message goes. The daemon points it at its own log as
// well as the terminal, because on some platforms the terminal is a socket into a
// journal that is not running (see internal/state/logging.go): an error that only went
// there would be lost exactly when it mattered.
var errorSink io.Writer = os.Stderr

func fatal(err error) {
	_, _ = fmt.Fprintln(errorSink, "error: "+err.Error())
	os.Exit(1)
}

func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, d)
}
