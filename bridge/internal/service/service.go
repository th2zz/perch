// Package service renders the supervisor units that keep the bridge alive.
//
// The units are the product's answer to a measured fact: herdr's startup hook
// is a one-shot command, not a supervisor (lab/bridge-probe; herdr docs call
// them "one-shot initialization commands rather than supervised daemons"). So
// process supervision is delegated to the operating system's own facility,
// exactly as any other long-running user service would be.
//
// The templates live next to this file and are embedded, so the installed unit
// and the reviewed template cannot drift apart.
package service

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

//go:embed units/perch-bridge.service.tmpl
var systemdTemplate string

//go:embed units/dev.perch.bridge.plist.tmpl
var launchdTemplate string

// Name is the service name used by systemd and in diagnostics.
const Name = "perch-bridge"

// data is what the templates are rendered with.
type data struct {
	Executable string
	LogPath    string
	// HerdrBinPath is where `herdr` was when this unit was installed.
	//
	// A service does not inherit the shell that installed it, and a user service's PATH is short:
	// measured on this machine, the bridge running under systemd could not find `herdr` at all
	// ("exec: herdr: executable file not found in $PATH"), which broke both the terminal's byte stream
	// and every `--machine` call to another computer — for anybody who had not noticed. The installer
	// runs in the user's own shell, so it is the one place that can answer this, and it answers by
	// writing the answer down.
	HerdrBinPath string
	// Path is the PATH the service should run with: the standard directories plus wherever this
	// machine keeps user binaries.
	Path string
}

// UnitPath returns where the unit belongs on this platform.
func UnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "systemd", "user", Name+".service"), nil
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "dev.perch.bridge.plist"), nil
	default:
		return "", fmt.Errorf("automatic start-up is not implemented for %s", runtime.GOOS)
	}
}

// Installed reports whether a unit file is present.
func Installed() bool {
	path, err := UnitPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Render builds the unit text for the current platform.
func Render(executable string) (string, error) {
	return RenderFor(runtime.GOOS, executable)
}

// RenderFor builds the unit text for a named platform.
//
// It exists so the other platform's startup file can be looked at — and *checked* —
// from this one: a plist cannot be run on Linux, and "we support macOS" deserves
// better evidence than a template that was read once. `platform` uses Go's GOOS
// spelling ("linux", "darwin").
func RenderFor(platform, executable string) (string, error) {
	source := systemdTemplate
	switch platform {
	case "darwin":
		source = launchdTemplate
	case "linux":
	default:
		return "", fmt.Errorf("automatic start-up is not implemented for %s", platform)
	}
	tmpl, err := template.New("unit").Parse(source)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	herdrPath, _ := exec.LookPath(herdr.Program)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data{
		Executable:   executable,
		LogPath:      filepath.Join(home, "Library", "Logs", "perch-bridge.log"),
		HerdrBinPath: herdrPath,
		Path: strings.Join([]string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
			"/usr/local/bin",
			"/usr/bin",
			"/bin",
			"/usr/sbin",
			"/sbin",
		}, ":"),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
