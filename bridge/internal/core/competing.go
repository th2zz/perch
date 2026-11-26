package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CompetingNotifier is another program already watching the user's agents and
// sending notifications about them.
type CompetingNotifier struct {
	PID    int
	Script string
}

// FindCompetingNotifier looks for a notifier the user set up before Perch.
//
// Why this is worth a check-up line: this product's audience is people who
// cobbled together a poll-and-notify script, and this machine is no exception —
// herdr-notify.service has been running here since before the bridge existed. If
// both run, every agent that blocks produces two notifications, and the honest
// reading of that from the phone is "Perch duplicates what I already have" rather
// than "Perch replaces it".
//
// The product must not disable it. That script is the user's working setup, and
// turning it off before Perch has proven itself would leave them with nothing. So
// this only reports, with the one command that settles it.
func FindCompetingNotifier() (CompetingNotifier, bool) {
	if runtime.GOOS != "linux" {
		// Reading process tables is not portable, and a guess would be worse than
		// saying nothing.
		return CompetingNotifier{}, false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return CompetingNotifier{}, false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := atoiOrZero(entry.Name())
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		cmdline := strings.ReplaceAll(strings.TrimRight(string(raw), "\x00"), "\x00", " ")
		lower := strings.ToLower(cmdline)

		// Deliberately narrow: a notifier that watches herdr. A broader match on
		// "notify" would flag unrelated things and train the user to ignore this line.
		if !strings.Contains(lower, "herdr-notify") {
			continue
		}
		script := ""
		for _, field := range strings.Fields(cmdline) {
			if strings.HasSuffix(field, ".py") {
				script = field
				break
			}
		}
		return CompetingNotifier{PID: pid, Script: script}, true
	}
	return CompetingNotifier{}, false
}

// UnitHint names the systemd unit a competing script probably belongs to, so the
// fix line can be a command rather than a description of a command.
func (c CompetingNotifier) UnitHint() string {
	if c.Script == "" {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(c.Script), ".py")
	if name == "" {
		return ""
	}
	return name + ".service"
}

func atoiOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
		if n > 1<<30 {
			return 0
		}
	}
	return n
}
