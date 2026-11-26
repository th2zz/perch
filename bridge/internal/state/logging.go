package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileLog is the bridge's own log, inside the state directory.
//
// It exists because the obvious place for a daemon's output is not always there. On
// this project's first-class platform the journal is not running at all (`systemd-
// journald.service` does not exist in that WSL distribution), so a service whose
// output goes to the journal leaves *nothing* behind when it fails — the user is told
// "the service is not running" with no way to find out why. Keeping the log next to
// the state means one place to look on every platform, and `perch-bridge logs` already
// prints this path.
const FileLog = "bridge.log"

// LogIfNotRedirected reports whether the bridge should open its own log file.
//
// When a supervisor already sends our output to a file (the plugin's detached start
// does, and launchd does), opening the same file again would duplicate every line.
// So the process that is being redirected says so, and the bridge trusts it.
func LogIfNotRedirected() bool {
	return os.Getenv(EnvLogRedirected) == ""
}

// EnvLogRedirected marks a process whose stdout/stderr already land in the log file.
const EnvLogRedirected = "PERCH_LOG_REDIRECTED"

// OpenLog opens (and if needed rotates) the bridge's own log.
//
// Rotation is by size and by one generation: a daemon that runs for months must not
// be able to fill a disk, and nobody is going to configure logrotate for something
// they installed with one command. Keeping one previous file is enough to look at
// what happened before the restart.
func OpenLog(dir string, maxBytes int64) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, FileLog)

	if info, err := os.Stat(path); err == nil && maxBytes > 0 && info.Size() > maxBytes {
		// Rename rather than truncate: the interesting lines are the ones just before
		// the restart, and they are exactly what a truncate would destroy.
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotating %s: %w", path, err)
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
