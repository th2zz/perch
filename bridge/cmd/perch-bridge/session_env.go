package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func userSessionEnv() []string {
	return userSessionEnvFor(os.Environ(), os.Getuid(), "/run/user")
}

// userSessionEnvFor is the part that can be tested: [uid] and [base] are arguments
// so a test can point at a directory it made, which is the only way to check both
// branches without a second machine.
func userSessionEnvFor(env []string, uid int, base string) []string {
	if lookupEnv(env, "XDG_RUNTIME_DIR") != "" {
		return env
	}
	dir := filepath.Join(base, fmt.Sprint(uid))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		// No user manager on this machine. Saying so is the caller's job; adding a
		// path that does not exist would only change the error message into a
		// different, more confusing one.
		return env
	}
	out := append(append([]string(nil), env...),
		"XDG_RUNTIME_DIR="+dir,
	)
	// The bus socket sits in the same place whenever there is one. Setting the
	// address as well saves systemctl from having to guess, and it is the address
	// systemd itself would have exported.
	if _, err := os.Stat(filepath.Join(dir, "bus")); err == nil &&
		lookupEnv(env, "DBUS_SESSION_BUS_ADDRESS") == "" {
		out = append(out, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(dir, "bus"))
	}
	return out
}

// lookupEnv is os.Getenv for a []string environment, because the value we are
// asking about is the one we are about to hand to a child process, not ours.
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
