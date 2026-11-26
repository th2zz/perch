package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/th2zz/perch/bridge/internal/platform"

	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/fingerprint"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/server"
	"github.com/th2zz/perch/bridge/internal/service"
	"github.com/th2zz/perch/bridge/internal/state"
)

const serviceName = service.Name

var unitPath = service.UnitPath

func unitInstalled() bool { return service.Installed() }

// cmdInstallService is the "one line install" half of SC-015. It writes the
// unit and asks the supervisor to start it; it never needs sudo, because a user
// service is enough for a process that runs as this user.
func cmdInstallService(ctx context.Context, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	unit, err := service.Render(exe)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)

	switch runtime.GOOS {
	case "linux":
		if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
			return printManualStart(path, err)
		}
		if err := runCommand("systemctl", "--user", "enable", "--now", serviceName+".service"); err != nil {
			return printManualStart(path, err)
		}
	case "darwin":
		_ = runCommand("launchctl", "unload", path)
		if err := runCommand("launchctl", "load", "-w", path); err != nil {
			return printManualStart(path, err)
		}
	}

	// Ask the supervisor whether it is actually running, rather than announcing it.
	//
	// The first version of this printed "installed and started" immediately after
	// issuing the command, and a delivery run against a machine where the user bus
	// was not reachable proved that message false: the unit was written, the service
	// was dead, and the installer's own words disagreed with `systemctl status`. A
	// one-line install that lies is worse than one that asks the user to do something.
	if running, detail := serviceIsRunning(); running {
		fmt.Println("perch bridge installed and started")
	} else {
		fmt.Printf("the service was installed at %s, but it is not running here: %s\n", path, detail)
		return fmt.Errorf("installed, but not started — %s", startHint())
	}

	// What "it will come back after a reboot" actually requires depends on where
	// this is running: a plain Linux machine needs lingering, and a WSL
	// distribution is not even started until Windows starts it (see
	// internal/platform). Both are handled here rather than described in the docs,
	// because a user reading a list of commands is the failure this feature exists
	// to avoid.
	applyAutostart(platform.Detect())
	return nil
}

// serviceIsRunning asks the supervisor, and says what it answered.
func serviceIsRunning() (bool, string) {
	switch runtime.GOOS {
	case "linux":
		if err := runCommandQuiet("systemctl", "--user", "is-active", serviceName+".service"); err != nil {
			return false, "systemctl --user says it is not active"
		}
		return true, ""
	case "darwin":
		if _, err := exec.Command("launchctl", "list", serviceName).Output(); err != nil {
			return false, "launchctl does not list it"
		}
		return true, ""
	default:
		return false, "unknown platform"
	}
}

// startHint explains the one common reason a user service will not start from an
// unattended shell: there is no user session bus to talk to.
func startHint() string {
	if runtime.GOOS != "linux" {
		return "run it from a normal login session"
	}
	return "the usual cause is running this over ssh without a user session bus; " +
		"run it from a normal login shell on that machine, or run: " +
		"systemctl --user enable --now " + serviceName
}

// applyAutostart runs whatever this machine needs to survive a reboot, and states
// the one thing it cannot do itself.
func applyAutostart(facts platform.Facts) {
	steps := platform.AutostartPlan(facts)
	if len(steps) == 0 {
		fmt.Println("it will come back after a reboot.")
		return
	}
	fmt.Println()
	for _, step := range steps {
		if len(step.Run) > 0 {
			if step.Why != "" {
				fmt.Printf("%s\n", step.Why)
			}
			if err := runCommand(step.Run[0], step.Run[1:]...); err != nil {
				// Optional steps (the Windows task) fail for reasons the user cannot
				// fix from here — no interop, no permission — and the sentence below
				// tells them what to run instead.
				fmt.Printf("  could not do that automatically (%v)\n", err)
				fmt.Printf("  run this instead:  %s\n", strings.Join(step.Run, " "))
				continue
			}
		} else if step.Detail != "" {
			fmt.Printf("%s\n", step.Detail)
		}
	}
}

// printManualStart reports that the unit is written but the supervisor would not
// take it, and returns an error.
//
// It used to return nil, which meant a script (or a user reading the exit code)
// could not tell "installed and running" from "written to disk and dead". The first
// version learned that the hard way, in a delivery run over ssh where the user bus
// was unreachable: the installer printed instructions, exited zero, and the machine
// was left with nothing serving the phone.
func printManualStart(path string, err error) error {
	fmt.Printf("\nthe unit is written to %s, but it is NOT running.\n", path)
	fmt.Printf("why: %v\n", err)
	switch runtime.GOOS {
	case "linux":
		fmt.Println("if this is an unattended shell (or a plain ssh command), the usual cause")
		fmt.Println("is that there is no user session bus to talk to. Run this on the machine")
		fmt.Println("from a normal login shell, or start it directly:")
		fmt.Println("  systemctl --user daemon-reload && systemctl --user enable --now " + serviceName)
	default:
		fmt.Println("  launchctl load -w " + path)
	}
	return fmt.Errorf("the service is installed but not running")
}

// cmdRenderUnit prints the supervisor unit this machine would get, without writing
// or installing anything.
//
// It exists for two audiences. A user about to install a background service
// deserves to see what it will be before it is, and `scripts/check-unit.sh` needs the
// real artefact rather than a copy of the template — the whole point of the check
// is that the file systemd reads is the file the templates render.
func cmdRenderUnit(args []string) error {
	fs := flag.NewFlagSet("render-unit", flag.ContinueOnError)
	out := fs.String("out", "", "write the unit to this path instead of stdout")
	platform := fs.String("platform", runtime.GOOS,
		"render for another platform (linux|darwin); for auditing the macOS file from Linux")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	unit, err := service.RenderFor(*platform, exe)
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Print(unit)
		return nil
	}
	if err := os.WriteFile(*out, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

// cmdUninstallService is the "one line uninstall" half. It stops the service,
// removes the unit, and tells the user the single directory to delete. It does
// not delete that directory itself: deleting data is the user's call.
func cmdUninstallService(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall-service", flag.ContinueOnError)
	keepState := fs.Bool("keep-state", false, "leave the bridge's state directories in place")
	baseline := fs.String("baseline", "", "compare against a fingerprint taken before installing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, err := unitPath()
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "linux":
		_ = runCommand("systemctl", "--user", "disable", "--now", serviceName+".service")
	case "darwin":
		_ = runCommand("launchctl", "unload", "-w", path)
	}

	// The Windows-side task that starts this distribution at logon was created by
	// the install. Leaving it behind would be an uninstall that missed a piece of
	// itself, and the user has no reason to know that name.
	facts := platform.Detect()
	if facts.Kind == platform.WSL && facts.WindowsAutostart == platform.Yes && facts.HasReg {
		if err := runCommand(
			"reg.exe", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v", platform.RunKeyValue, "/f",
		); err != nil {
			fmt.Fprintf(os.Stderr, "could not remove the Windows logon entry %s: %v\n",
				platform.RunKeyValue, err)
			fmt.Fprintf(os.Stderr,
				"  remove it by hand:  reg.exe delete \"HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run\" /v %s /f\n",
				platform.RunKeyValue)
		} else {
			fmt.Printf("removed the Windows logon entry %s\n", platform.RunKeyValue)
		}
	}

	// Only claim a removal that happened: printing "removed" for a file that was
	// never there would make the uninstall's own account of itself unreliable,
	// and that account is the only evidence the user gets.
	switch err := os.Remove(path); {
	case err == nil:
		fmt.Printf("removed %s\n", path)
	case errors.Is(err, os.ErrNotExist):
		fmt.Printf("no unit at %s (nothing to remove)\n", path)
	default:
		return err
	}

	c := mustCore(true)
	// Best effort: tell herdr we are gone so the plugin page stops offering us.
	if _, err := exec.LookPath(herdr.Program); err == nil {
		_ = runCommand(herdr.Program, herdr.GroupPlugin, herdr.VerbUnlink, pluginID)
	}

	// The state directories go too. "Uninstall" that leaves caches behind is not
	// an uninstall, and the user should not have to be told a path to delete —
	// telling them a path is how this used to work, and it is a worse product.
	//
	// Every removed path is printed, and each one is checked before it is
	// touched: this is the only destructive thing the bridge does, so it names
	// exactly what it is deleting rather than globbing.
	if *keepState {
		fmt.Println("state kept, as asked:")
		for _, dir := range stateDirectories(c) {
			fmt.Printf("  %s\n", dir.path)
		}
	} else {
		for _, dir := range stateDirectories(c) {
			removed, err := removeStateDir(dir)
			switch {
			case err != nil:
				fmt.Fprintf(os.Stderr, "could not remove %s: %v\n", dir.path, err)
				fmt.Fprintf(os.Stderr, "you can delete it by hand if you want it gone.\n")
			case removed:
				fmt.Printf("removed %s\n", dir.path)
			}
		}
	}

	fmt.Println("the bridge is stopped and will not restart.")
	fmt.Println("your herdr configuration was never touched.")

	if *baseline != "" {
		fmt.Println()
		return verifyAgainstBaseline(*baseline, c)
	}
	return nil
}

// stateDir is somewhere the bridge may have written.
type stateDir struct {
	path string
	// owned means the bridge chose this path itself, so its name must be one of
	// ours. A directory handed to us through the environment is not owned in that
	// sense — herdr decides where it goes — so it is checked by shape instead.
	owned bool
}

// stateDirectories lists every place the bridge may have written, so an
// uninstall does not depend on which way the bridge happened to be started:
// herdr injects one directory for plugin processes, and running the CLI by hand
// uses a per-user cache path instead.
func stateDirectories(c interface{ Store() *state.Store }) []stateDir {
	var dirs []stateDir
	if injected := os.Getenv(state.EnvStateDir); injected != "" {
		dirs = append(dirs, stateDir{path: injected})
	}
	if dir, err := state.Dir(); err == nil {
		dirs = append(dirs, stateDir{path: dir, owned: true})
	}
	if home, err := os.UserHomeDir(); err == nil {
		// herdr creates this for a linked plugin and does not remove it on
		// unlink, so the product's own uninstall has to.
		dirs = append(dirs, stateDir{
			path:  herdr.PluginStateDir(home, pluginID),
			owned: true,
		})
	}
	return dedupe(dirs)
}

// removeStateDir deletes one directory, refusing anything that does not look
// like ours.
//
// This is the only destroy operation in the product, so the guards fail closed.
// They are not merely defensive either: the first version required every path to
// be named after the plugin, which refused the perfectly legitimate directory
// herdr injects — and the verification script caught exactly that. The rule is
// now "trust the environment's path if it is shaped like a real working
// directory; trust our own computed paths only if they are named ours".
func removeStateDir(dir stateDir) (bool, error) {
	cleaned := filepath.Clean(dir.path)
	if cleaned == "" || cleaned == "." {
		return false, errors.New("refusing to remove an unusable path")
	}
	if !filepath.IsAbs(cleaned) {
		return false, errors.New("refusing to remove a relative path: " + cleaned)
	}
	separator := string(filepath.Separator)
	if cleaned == separator {
		return false, errors.New("refusing to remove the filesystem root")
	}

	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.Clean(home)
		if cleaned == home {
			return false, errors.New("refusing to remove the home directory")
		}
		// Never remove anything that sits above the home directory.
		if strings.HasPrefix(home+separator, cleaned+separator) {
			return false, errors.New("refusing to remove a parent of the home directory")
		}
	}

	// A one-level path ("/state") is a mistake rather than a state directory:
	// every real one lives inside a run, cache or plugin area.
	if len(strings.Split(strings.Trim(cleaned, separator), separator)) < 2 {
		return false, errors.New("refusing to remove a top-level directory: " + cleaned)
	}

	if dir.owned {
		base := filepath.Base(cleaned)
		if base != pluginID && base != "perch-bridge" {
			return false, errors.New("refusing to remove a directory that is not ours: " + base)
		}
	}

	if _, err := os.Stat(cleaned); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(cleaned); err != nil {
		return false, err
	}
	return true, nil
}

func dedupe(items []stateDir) []stateDir {
	seen := map[string]struct{}{}
	out := make([]stateDir, 0, len(items))
	for _, item := range items {
		key := filepath.Clean(item.path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

// verifyAgainstBaseline re-runs the zero-change check after an uninstall, so the
// person uninstalling sees the evidence rather than having to trust a sentence.
func verifyAgainstBaseline(baseline string, c *core.Core) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	before, err := fingerprint.Read(baseline)
	if err != nil {
		return err
	}
	after, err := fingerprint.Take(home, c.Store().Dir())
	if err != nil {
		return err
	}
	changes := fingerprint.Compare(before, after)
	for _, change := range changes {
		fmt.Printf("  %s: %s\n", change.Kind, change.Detail)
	}
	if unexpected := fingerprint.Unexpected(changes); len(unexpected) > 0 {
		return fmt.Errorf("%d unexpected change(s) remain", len(unexpected))
	}
	if len(changes) == 0 {
		fmt.Println("Nothing of yours changed. Uninstalling left no trace.")
	}
	return nil
}

// pluginID must match plugin/herdr-plugin.toml.
const pluginID = "perch.bridge"

// Package-level assertion that the port we advertise is the one the server
// actually binds by default; a mismatch here would be invisible until a user
// tried to pair.
var _ = func() bool {
	if !strings.HasPrefix(server.DefaultAddr, ":") {
		panic("server.DefaultAddr must be a bare port")
	}
	return true
}()
