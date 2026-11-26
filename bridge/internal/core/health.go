package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/state"

	"github.com/th2zz/perch/bridge/internal/platform"
)

// HealthStatus is the verdict for one check.
type HealthStatus string

const (
	HealthOK      HealthStatus = "ok"
	HealthWarn    HealthStatus = "warn"
	HealthFail    HealthStatus = "fail"
	HealthUnknown HealthStatus = "unknown"
)

// String lets a status be printed directly.
func (s HealthStatus) String() string { return string(s) }

// HealthItem is one row of the check-up.
//
// Every non-OK row MUST carry a Fix that is a command the user can run, because
// spec SC-013 counts "please edit some file" style advice as a defect, and
// FR-015 forbids silent failure.
type HealthItem struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Status HealthStatus `json:"status"`
	Detail string       `json:"detail"`
	Fix    string       `json:"fix,omitempty"`
}

// Health is the full check-up.
type Health struct {
	CheckedAt    time.Time    `json:"checked_at"`
	HerdrVersion string       `json:"herdr_version,omitempty"`
	Protocol     uint32       `json:"protocol,omitempty"`
	Items        []HealthItem `json:"items"`
	// Overall is ok only when no item failed.
	Overall HealthStatus `json:"overall"`
}

// CheckHealth runs the seven checks from the bridge-plugin contract §3.
func (c *Core) CheckHealth(ctx context.Context) Health {
	h := Health{CheckedAt: time.Now()}

	// 1. Protocol/version compatibility, asked of the running server.
	if compat, err := herdr.CheckCompat(ctx, c.api); err != nil {
		h.Items = append(h.Items, HealthItem{
			ID: "herdr_protocol", Title: "herdr version and protocol",
			Status: HealthFail,
			Detail: "cannot ask herdr which protocol it speaks: " + err.Error(),
			Fix:    "start herdr, then run: herdr status",
		})
	} else {
		h.HerdrVersion, h.Protocol = compat.Version, compat.Protocol
		item := HealthItem{
			ID: "herdr_protocol", Title: "herdr version and protocol",
			Status: HealthOK,
			Detail: "herdr " + compat.Version + ", protocol " + itoa(int(compat.Protocol)),
		}
		if !compat.OK {
			item.Status = HealthFail
			item.Detail = compat.Reason
			item.Fix = "herdr update"
		}
		h.Items = append(h.Items, item)
	}

	// 2. Socket reachable.
	if _, err := c.api.Snapshot(ctx); err != nil {
		h.Items = append(h.Items, HealthItem{
			ID: "socket", Title: "herdr socket",
			Status: HealthFail,
			Detail: "cannot read the session over " + c.api.Path() + ": " + err.Error(),
			Fix:    "start herdr, or check HERDR_SOCKET_PATH",
		})
	} else {
		h.Items = append(h.Items, HealthItem{
			ID: "socket", Title: "herdr socket",
			Status: HealthOK,
			Detail: "reading " + c.api.Path(),
		})
	}

	// 3. Event subscription.
	switch {
	case c.opts.DisableSubscriptions:
		h.Items = append(h.Items, HealthItem{
			ID: "events", Title: "live event subscription",
			Status: HealthUnknown,
			Detail: "not checked in this mode",
		})
	case c.SubscriptionHealthy():
		panes := 0
		if m := c.Mirror(); m != nil {
			panes = len(m.Panes)
		}
		h.Items = append(h.Items, HealthItem{
			ID: "events", Title: "live event subscription",
			Status: HealthOK,
			Detail: "subscribed; tracking " + itoa(panes) + " panes",
		})
	default:
		h.Items = append(h.Items, HealthItem{
			ID: "events", Title: "live event subscription",
			Status: HealthWarn,
			Detail: "the event stream is not currently established; the bridge is retrying with backoff",
			Fix:    "perch-bridge logs",
		})
	}

	// 4. Is the bridge actually serving? Not "did this program start" — a one-shot
	//    `doctor` is also a running process, and answering with itself made this
	//    check say everything was fine while nothing was listening.
	if pid, running := c.DaemonProcess(); running {
		detail := "serving as pid " + itoa(pid)
		if pid == os.Getpid() {
			detail = "this process is the bridge (pid " + itoa(pid) + ")"
		}
		h.Items = append(h.Items, HealthItem{
			ID: "daemon", Title: "bridge running",
			Status: HealthOK,
			Detail: detail + ", state dir " + c.store.Dir(),
		})
	} else {
		// A failure rather than a warning: with nothing serving, the phone receives
		// nothing at all. Reporting that as merely degraded would be the kind of
		// reassurance that costs someone an afternoon.
		h.Items = append(h.Items, HealthItem{
			ID: "daemon", Title: "bridge running",
			Status: HealthFail,
			Detail: "nothing is serving on the phone's behalf",
			// Two causes look identical from here — no service installed, or a service
			// that failed and gave up (see the start limits in the unit) — so the fix
			// names the way to find out first. `logs` shows the unit's state and the
			// tail of the bridge's own log, which is where the reason is.
			Fix: "run `perch-bridge logs` to see why, then `perch-bridge install-service`",
		})
	}

	// 5. Autostart configured.
	h.Items = append(h.Items, autostartItem())

	// 6. Phone reachability.
	//
	// A one-shot (`doctor` from a shell) has no memory of the phone: the daemon does the
	// talking. So when this process has seen nothing, the answer comes from what the
	// daemon wrote down — including how stale that record is, because "the phone asked
	// 20 minutes ago" and "the phone asked just now" are different statements.
	last := c.LastClientSeen()
	stale := time.Duration(0)
	if last.IsZero() {
		if seen, age, ok := c.PersistedClientSeen(); ok {
			last, stale = seen, age
		}
	}
	if !last.IsZero() {
		h.Items = append(h.Items, HealthItem{
			ID: "phone", Title: "phone reachability",
			Status: HealthOK,
			Detail: "last request from the app " + HumanAgo(time.Since(last)) + " ago" +
				recordedNote(stale) + requestRateNote(c, stale),
		})
	} else {
		h.Items = append(h.Items, HealthItem{
			ID: "phone", Title: "phone reachability",
			Status: HealthWarn,
			Detail: "no request from the app yet",
			Fix:    "open Perch on your phone",
		})
	}

	// 7. Push registration.
	if c.PushConfigured() {
		h.Items = append(h.Items, HealthItem{
			ID: "push", Title: "push notifications",
			Status: HealthOK,
			Detail: "a push gateway is configured",
		})
	} else {
		h.Items = append(h.Items, HealthItem{
			ID: "push", Title: "push notifications",
			Status: HealthWarn,
			Detail: "no push gateway configured, so the phone is only told about requests while the app is open",
			Fix:    "perch-bridge pair --with-push",
		})
	}

	// 8. Nothing else is already doing this job. Reported last because it is the
	//    one check that is about the user's own setup rather than about Perch
	//    working: two notifiers is not a fault, it is a surprise waiting to happen.
	if competing, found := FindCompetingNotifier(); found {
		fix := "stop it when you are ready: systemctl --user disable --now " + competing.UnitHint()
		if competing.UnitHint() == "" {
			fix = "stop it when you are ready, so you do not get two notifications per event"
		}
		h.Items = append(h.Items, HealthItem{
			ID:     "competing_notifier",
			Title:  "one notifier at a time",
			Status: HealthWarn,
			Detail: filepath.Base(competing.Script) + " (pid " + itoa(competing.PID) + ") is also watching your agents, " +
				"so while both run you will get two notifications for the same event",
			Fix: fix,
		})
	} else {
		h.Items = append(h.Items, HealthItem{
			ID:     "competing_notifier",
			Title:  "one notifier at a time",
			Status: HealthOK,
			Detail: "nothing else is notifying you about your agents",
		})
	}

	h.Overall = HealthOK
	for _, item := range h.Items {
		if item.Status == HealthFail {
			h.Overall = HealthFail
			break
		}
		if item.Status == HealthWarn && h.Overall == HealthOK {
			h.Overall = HealthWarn
		}
	}
	return h
}

func autostartItem() HealthItem {
	switch runtime.GOOS {
	case "linux":
		path := filepath.Join(userHome(), ".config", "systemd", "user", "perch-bridge.service")
		if _, err := os.Stat(path); err != nil {
			return HealthItem{
				ID: "autostart", Title: "starts automatically",
				Status: HealthWarn,
				Detail: "no systemd user unit found, so the bridge will not come back after a restart",
				Fix:    "perch-bridge install-service",
			}
		}
		if isWSL() {
			// The unit is not enough on WSL: the distribution itself is only started
			// when something enters it, and after a Windows reboot nothing does. The
			// check-up answers that with facts rather than with a paragraph — is
			// lingering on, and is the Windows-side task there — because those are
			// the two things that are actually missing when this goes wrong.
			facts := platform.Detect()
			if facts.Linger == platform.Yes && facts.WindowsAutostart == platform.Yes {
				return HealthItem{
					ID: "autostart", Title: "starts automatically",
					Status: HealthOK,
					Detail: "systemd user unit at " + path +
						", lingering on, and Windows starts this distribution at logon",
				}
			}
			detail := "the systemd user unit is installed at " + path + ", but "
			switch {
			case facts.Linger != platform.Yes && facts.WindowsAutostart == platform.Yes:
				detail += "this user's services stop when the last WSL terminal closes"
			case facts.Linger == platform.Yes && facts.WindowsAutostart != platform.Yes:
				detail += "Windows never starts this distribution after a reboot, so nothing runs the unit"
			default:
				detail += "services stop with the last WSL terminal, and Windows never " +
					"starts this distribution after a reboot"
			}
			return HealthItem{
				ID: "autostart", Title: "starts automatically",
				Status: HealthWarn,
				Detail: detail + " (a WSL limitation, not a Perch defect)",
				Fix:    "perch-bridge install-service   —   it detects this and fixes what it can",
			}
		}
		// A plain Linux machine has one version of this gap: a user service dies
		// with the login session unless lingering is on, which is exactly what a
		// server without a logged-in user looks like.
		if facts := platform.Detect(); facts.Linger == platform.No {
			return HealthItem{
				ID: "autostart", Title: "starts automatically",
				Status: HealthWarn,
				Detail: "the systemd user unit is installed at " + path +
					", but lingering is off, so it stops when your last session ends",
				Fix: "sudo loginctl enable-linger " + facts.User,
			}
		}
		return HealthItem{
			ID: "autostart", Title: "starts automatically",
			Status: HealthOK,
			Detail: "systemd user unit installed at " + path,
		}
	case "darwin":
		path := filepath.Join(userHome(), "Library", "LaunchAgents", "dev.perch.bridge.plist")
		if _, err := os.Stat(path); err == nil {
			return HealthItem{
				ID: "autostart", Title: "starts automatically",
				Status: HealthOK,
				Detail: "launchd agent installed at " + path,
			}
		}
		return HealthItem{
			ID: "autostart", Title: "starts automatically",
			Status: HealthWarn,
			Detail: "no launchd agent found, so the bridge will not come back after a restart",
			Fix:    "perch-bridge install-service",
		}
	default:
		return HealthItem{
			ID: "autostart", Title: "starts automatically",
			Status: HealthUnknown,
			Detail: "automatic start-up is not implemented for " + runtime.GOOS,
		}
	}
}

// isWSL reports whether we are running inside Windows Subsystem for Linux.
func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

// ---------------------------------------------------------------- push config

// PushConfig is the state file describing where to send notifications.
type PushConfig struct {
	// GatewayURL is the push gateway's endpoint. The gateway is a central
	// service because the FCM service-account key must not be copied onto every
	// user's machine (research §10).
	GatewayURL string `json:"gateway_url"`
	// GatewayToken authenticates this host to the gateway. It is a different
	// secret from the app's session token, so leaking one does not leak both.
	GatewayToken string `json:"gateway_token,omitempty"`
	// DeviceToken is the phone's FCM registration token.
	DeviceToken string `json:"device_token"`
	// DeviceTokenUpdated is when it was last refreshed.
	DeviceTokenUpdated time.Time `json:"device_token_updated,omitempty"`
}

// PushConfigured reports whether notifications can leave the host.
func (c *Core) PushConfigured() bool {
	cfg, err := c.PushConfig()
	return err == nil && cfg.GatewayURL != "" && cfg.DeviceToken != ""
}

// PushConfig reads the push configuration.
func (c *Core) PushConfig() (PushConfig, error) {
	var cfg PushConfig
	if err := c.store.ReadJSON(state.FilePush, &cfg); err != nil {
		return PushConfig{}, err
	}
	return cfg, nil
}

// SavePushConfig stores the push configuration.
func (c *Core) SavePushConfig(cfg PushConfig) error {
	cfg.DeviceTokenUpdated = time.Now()
	return c.store.WriteJSON(state.FilePush, cfg)
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// HumanAgo renders a duration the way a person says it: "12s", "4m", "2h".
//
// Exported because `perch-bridge status` reports the phone's reachability too, and two
// spellings of the same duration in two commands is how "12s" and "12 seconds" end up
// meaning different things to whoever reads both.
func HumanAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	default:
		return itoa(int(d.Hours())) + "h"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// requestRateNote says how much the phone is asking for, when that is worth knowing.
//
// A healthy phone asks a handful of times a minute while somebody is using it, and
// almost nothing while they are not. A number far above that is the shape of a loop —
// the bug class that once had the app and the bridge refreshing each other for ever,
// visible on a laptop only as "warm and busy".
// countOf says "1 request", not "1 requests": a number next to a noun is a sentence
// someone reads, and the product's other counts already take this seriously.
func countOf(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// recordedNote says when the daemon last wrote the record down, when that is not this
// process. Without it the age above would look live to someone reading it.
func recordedNote(stale time.Duration) string {
	if stale == 0 {
		return ""
	}
	return " (recorded " + HumanAgo(stale) + " ago by the running bridge)"
}

func requestRateNote(c *Core, stale time.Duration) string {
	lastMinute, lastTen := c.ClientRequestRate()
	if lastTen == 0 {
		return ""
	}
	if lastMinute > 60 {
		return fmt.Sprintf("; %s in the last minute, which looks like a loop (see `perch-bridge logs`)",
			countOf(lastMinute, "request"))
	}
	// The daemon records this at most every few seconds, so a number read by a one-shot
	// command describes the moment of that record, not this instant. Saying which is the
	// difference between a measurement and a claim.
	asOf := ""
	if stale > 0 {
		asOf = " as of that record"
	}
	return fmt.Sprintf("; %s in the last minute, %s in the last ten%s",
		countOf(lastMinute, "request"), countOf(lastTen, "request"), asOf)
}
