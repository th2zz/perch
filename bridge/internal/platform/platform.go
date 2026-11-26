// Package platform decides what "keep the bridge running after a reboot" means on
// the machine it is running on.
//
// The spec requires the same one-line install on three backends (WSL, macOS, cloud
// Linux) and forbids handing the user a list of commands to type (SC-015, SC-013).
// The three backends differ in exactly two places, and both are mechanical:
//
//   - **A user service needs a session that persists.** On a normal Linux machine
//     `systemctl --user enable --now` is enough only if lingering is on;
//     otherwise the service dies with the login session. On WSL the session ends
//     when the last terminal closes, so lingering is not a nicety.
//   - **A WSL distribution is not running until something enters it.** After a
//     Windows reboot there is no Linux process to start the unit at all, so the
//     chain has to begin on the Windows side: one scheduled task at logon.
//
// Everything here is a pure function of a Facts value, so the decisions can be
// tested for all three backends on any one of them. The caller supplies the facts
// by probing the machine.
package platform

import "fmt"

// Kind is the environment the bridge finds itself in.
type Kind string

const (
	Linux   Kind = "linux"
	WSL     Kind = "wsl"
	Darwin  Kind = "darwin"
	Unknown Kind = "unknown"
)

// Tri is a three-valued answer: a question we asked and got an answer to, versus
// one we could not ask.
//
// It is not a nicety. "Lingering is off" and "there is no loginctl here" call for
// different sentences, and collapsing them into a bool is how a program tells the
// user to run a command that does not exist on their machine.
type Tri int

const (
	UnknownTri Tri = iota
	No
	Yes
)

// Facts is what the machine looks like. Every field is something the caller
// probed; nothing here is guessed.
type Facts struct {
	Kind Kind
	User string
	// Systemd reports whether a systemd user session is available at all.
	Systemd bool
	// Linger says whether this user's services outlive their login session.
	Linger Tri
	// HasLoginctl and HasReg say whether the tools to fix the two gaps exist.
	HasLoginctl bool
	HasReg      bool
	// WindowsAutostart says whether Windows already starts this distribution at
	// logon.
	WindowsAutostart Tri
	Distro           string
}

// Step is one thing the installer does or says, in order.
//
// A step either runs a command or prints one. The distinction is the whole point
// of the package: what can be automated is automated, and what needs a password
// this process does not have is stated once, with the reason attached.
type Step struct {
	// Why explains the step in the user's terms, for the line of output.
	Why string
	// Run is the command to execute; when nil, Detail is printed instead.
	Run []string
	// Detail is the sentence (and command) shown when Run is nil.
	Detail string
	// Optional marks a step whose failure is not worth stopping for.
	Optional bool
}

// RunKeyValue is the name of the Windows per-user autostart entry that starts this
// distribution at logon.
//
// Measured, not assumed (2026-10-13, this machine): creating a *scheduled task*
// from inside WSL fails with "Access is denied" because that needs elevation, while
// the per-user Run key is writable by the same user and is the ordinary Windows
// way to start something at logon. So the Run key is what the installer writes, and
// the elevated command is only what it prints when even that is refused.
const RunKeyValue = "PerchBridge"

// runKeyPath is the per-user autostart key, spelled the way reg.exe wants it.
const runKeyPath = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// AutostartPlan returns the steps that make the bridge come back after a reboot on
// this machine, in order, skipping the ones that are already satisfied.
func AutostartPlan(f Facts) []Step {
	switch f.Kind {
	case Darwin:
		// launchd needs no extra step: a LaunchAgent in the user's own library
		// starts at login, and macOS runs the user's session for as long as they are
		// logged in.
		return nil
	case WSL:
		steps := linuxSteps(f)
		steps = append(steps, wslSteps(f)...)
		return steps
	case Linux:
		return linuxSteps(f)
	default:
		return []Step{{
			Detail: "automatic start-up is not implemented for this platform; " +
				"run the bridge under your own supervisor",
		}}
	}
}

func linuxSteps(f Facts) []Step {
	if !f.Systemd {
		return []Step{{
			Detail: "this machine has no systemd user session, so there is nothing to " +
				"enable; run `perch-bridge run` under whatever supervises your services",
		}}
	}
	var steps []Step
	if f.Linger != Yes {
		command := fmt.Sprintf("sudo loginctl enable-linger %s", f.User)
		detail := "without lingering, a user service stops when your last session ends"
		if f.Kind == WSL {
			detail = "without lingering, the bridge stops as soon as you close the last " +
				"WSL terminal"
		}
		step := Step{Detail: detail + "; run once:  " + command}
		if f.HasLoginctl {
			// It needs a password, so it is printed rather than attempted. Asking
			// for sudo in a one-line install is worse than one clear sentence.
			step.Why = "lingering is off"
		} else {
			step.Detail = detail + "; this machine has no loginctl, so check how your " +
				"distribution keeps user services alive"
		}
		steps = append(steps, step)
	}
	return steps
}

func wslSteps(f Facts) []Step {
	distro := f.Distro
	if distro == "" {
		distro = "(your distribution)"
	}
	start := "wsl.exe -d " + distro + " -e true"
	command := []string{
		"reg.exe", "add", runKeyPath, "/v", RunKeyValue, "/t", "REG_SZ", "/d", start, "/f",
	}
	why := "Windows starts this distribution at logon (a WSL distribution stays " +
		"stopped after a Windows reboot until something enters it)"

	if f.WindowsAutostart == Yes {
		return nil
	}
	if f.WindowsAutostart == UnknownTri || !f.HasReg {
		// No way to ask Windows from here: say the command rather than pretending.
		return []Step{{
			Detail: "Windows cannot be reached from here, so this one step cannot be done " +
				"for you. After a Windows reboot this distribution stays stopped until " +
				"something enters it. Add it to your Windows logon by running this in an " +
				"**administrator** PowerShell:  schtasks /Create /F /TN " + RunKeyValue +
				" /SC ONLOGON /TR \"" + start + "\"",
		}}
	}
	return []Step{{
		Why:      why,
		Run:      command,
		Optional: true,
	}}
}
