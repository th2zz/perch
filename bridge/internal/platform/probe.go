package platform

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Detect inspects the machine. It is separate from the decisions above so that the
// decisions can be tested on any platform: this file is the only part that touches
// the system.
func Detect() Facts {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		user, _ = os.UserHomeDir()
	}
	f := Facts{Kind: kindOn(runtime.GOOS), User: user, Distro: os.Getenv("WSL_DISTRO_NAME")}

	switch f.Kind {
	case Linux, WSL:
		// systemd's own marker for "there is a user session manager here". Checking
		// for it is what keeps a container or a stripped VM from being told to run
		// systemctl commands that cannot work.
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			f.Systemd = true
		}
		if _, err := exec.LookPath("loginctl"); err == nil {
			f.HasLoginctl = true
			if out, err := exec.Command("loginctl", "show-user", user, "--property=Linger").Output(); err == nil {
				f.Linger = triFromEquals(string(out))
			}
		}
	case Darwin:
		f.Systemd = false
	}

	if f.Kind == WSL {
		if _, err := exec.LookPath("reg.exe"); err == nil {
			f.HasReg = true
		}
		f.WindowsAutostart = windowsAutostartState()
	}
	return f
}

// kindOn decides which environment a GOOS describes.
//
// WSL is Linux to Go, so it has to be recognised from the kernel's own description
// of itself (`/proc/version` says "microsoft-standard-WSL2") or from the variable
// WSL sets in every process it starts.
func kindOn(goos string) Kind {
	switch goos {
	case "darwin":
		return Darwin
	case "linux":
		if os.Getenv("WSL_DISTRO_NAME") != "" {
			return WSL
		}
		if body, err := os.ReadFile("/proc/version"); err == nil {
			lower := strings.ToLower(string(body))
			if strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl") {
				return WSL
			}
		}
		return Linux
	default:
		return Unknown
	}
}

// windowsAutostartState asks Windows whether it already starts this distribution
// at logon.
//
// It goes through WSL's interop, which means the answer can be "I could not ask"
// rather than "no": a distribution can be configured without Windows tools on the
// path, and that is a different situation from an entry that is missing.
func windowsAutostartState() Tri {
	if _, err := exec.LookPath("reg.exe"); err != nil {
		return UnknownTri
	}
	err := exec.Command("reg.exe", "query", runKeyPath, "/v", RunKeyValue).Run()
	if err == nil {
		return Yes
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// reg.exe answers "there is no such value" with a non-zero status, which is
		// the answer we wanted.
		return No
	}
	return UnknownTri
}

func triFromEquals(output string) Tri {
	_, value, found := strings.Cut(strings.TrimSpace(output), "=")
	if !found {
		return UnknownTri
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes":
		return Yes
	case "no":
		return No
	}
	return UnknownTri
}
