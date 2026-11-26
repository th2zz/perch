package platform

import (
	"strings"
	"testing"
)

// summarise renders a plan as "run: <command>" / "say: <detail>" lines, which is
// what the assertions below read like.
func summarise(steps []Step) []string {
	var out []string
	for _, step := range steps {
		if len(step.Run) > 0 {
			out = append(out, "run: "+strings.Join(step.Run, " ")+"  —  "+step.Why)
			continue
		}
		out = append(out, "say: "+step.Detail)
	}
	return out
}

func contains(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestACloudLinuxMachineNeedsOneThing: a normal Linux box with systemd has exactly
// one gap, and it is the one people never guess — a user service stops when the
// last session ends unless lingering is on.
func TestACloudLinuxMachineNeedsOneThing(t *testing.T) {
	ready := Facts{Kind: Linux, User: "alice", Systemd: true, Linger: Yes, HasLoginctl: true}
	if steps := AutostartPlan(ready); len(steps) != 0 {
		t.Errorf("a machine with lingering on needs no advice, got %v", summarise(steps))
	}

	notLingering := Facts{Kind: Linux, User: "alice", Systemd: true, Linger: No, HasLoginctl: true}
	steps := summarise(AutostartPlan(notLingering))
	if !contains(steps, "sudo loginctl enable-linger alice") {
		t.Errorf("the command that fixes it is missing: %v", steps)
	}
	if !contains(steps, "stops when your last session ends") {
		t.Errorf("the reason is missing, so the command looks arbitrary: %v", steps)
	}
	// It needs a password this process does not have, so it is said rather than run.
	for _, step := range AutostartPlan(notLingering) {
		if len(step.Run) > 0 {
			t.Errorf("running %v would ask for a password mid-install", step.Run)
		}
	}
}

// TestNoSystemdIsNotASystemdProblem: a container or a stripped VM has no user
// session manager, and telling someone to run systemctl there is advice they
// cannot take.
func TestNoSystemdIsNotASystemdProblem(t *testing.T) {
	facts := Facts{Kind: Linux, User: "alice", Systemd: false}
	steps := summarise(AutostartPlan(facts))
	if len(steps) != 1 {
		t.Fatalf("steps = %v", steps)
	}
	if !contains(steps, "no systemd user session") {
		t.Errorf("the answer does not say why: %v", steps)
	}
	if strings.Contains(steps[0], "systemctl") {
		t.Errorf("it suggests a command this machine does not have: %v", steps)
	}
}

// TestWSLNeedsTheWindowsSideStep: inside WSL the unit is not enough, because after
// a Windows reboot nothing starts the distribution at all. This is the gap the
// product's own documentation calls its number-one hole, and the installer closes
// it instead of describing it.
func TestWSLNeedsTheWindowsSideStep(t *testing.T) {
	facts := Facts{
		Kind: WSL, User: "alice", Systemd: true, Linger: Yes, HasLoginctl: true,
		HasReg: true, WindowsAutostart: No, Distro: "Ubuntu-24.04",
	}
	steps := summarise(AutostartPlan(facts))
	if len(steps) != 1 {
		t.Fatalf("steps = %v", steps)
	}
	line := steps[0]
	for _, want := range []string{"reg.exe", "add", runKeyPath, "/v", RunKeyValue, "Ubuntu-24.04"} {
		if !strings.Contains(line, want) {
			t.Errorf("the Windows step is missing %q: %s", want, line)
		}
	}
	if !contains(steps, "logon") {
		t.Errorf("the step does not say when it runs: %v", steps)
	}
}

func TestWSLWithEverythingAlreadyDoneSaysNothing(t *testing.T) {
	facts := Facts{
		Kind: WSL, User: "alice", Systemd: true, Linger: Yes, HasLoginctl: true,
		HasReg: true, WindowsAutostart: Yes, Distro: "Ubuntu-24.04",
	}
	if steps := AutostartPlan(facts); len(steps) != 0 {
		t.Errorf("a machine that is already handled should get no advice: %v", summarise(steps))
	}
}

// TestWSLWithoutWindowsOnThePathPrintsTheCommand: interop can be turned off, and
// "I could not ask Windows" is a different situation from "the task is missing".
// Pretending it was one of them is how a user ends up with an autostart that never
// works and no way to find out why.
func TestWSLWithoutWindowsOnThePathPrintsTheCommand(t *testing.T) {
	facts := Facts{
		Kind: WSL, User: "alice", Systemd: true, Linger: Yes, HasLoginctl: true,
		WindowsAutostart: UnknownTri, Distro: "Ubuntu-24.04",
	}
	steps := summarise(AutostartPlan(facts))
	if len(steps) != 1 || !strings.HasPrefix(steps[0], "say: ") {
		t.Fatalf("steps = %v", steps)
	}
	if !strings.Contains(steps[0], "schtasks /Create") {
		t.Errorf("the manual command is missing: %v", steps)
	}
	for _, step := range AutostartPlan(facts) {
		if len(step.Run) > 0 {
			t.Errorf("it tried to run Windows commands that are not there: %v", step.Run)
		}
	}
}

// TestMacNeedsNoExtraStep: a LaunchAgent in the user's own library starts at login
// and macOS keeps the user session alive, so there is no second gap to close.
func TestMacNeedsNoExtraStep(t *testing.T) {
	if steps := AutostartPlan(Facts{Kind: Darwin, User: "alice"}); len(steps) != 0 {
		t.Errorf("macOS should need no extra step, got %v", summarise(steps))
	}
}

func TestAnUnsupportedPlatformSaysSoPlainly(t *testing.T) {
	steps := summarise(AutostartPlan(Facts{Kind: Unknown}))
	if len(steps) != 1 || !strings.Contains(steps[0], "not implemented") {
		t.Errorf("steps = %v", steps)
	}
}

// TestTheKernelDecidesWhetherThisIsWSL: Go reports WSL as plain Linux, so the
// answer has to come from the kernel's own description of itself.
func TestTheKernelDecidesWhetherThisIsWSL(t *testing.T) {
	if got := kindOn("darwin"); got != Darwin {
		t.Errorf("darwin = %q", got)
	}
	if got := kindOn("windows"); got != Unknown {
		t.Errorf("windows = %q", got)
	}
	// On a Linux test host, whichever of the two it is must match what the machine
	// actually is: this test runs inside WSL on the development machine, and that is
	// the case the detection exists for.
	got := kindOn("linux")
	if got != Linux && got != WSL {
		t.Errorf("linux = %q", got)
	}
	if detected := Detect(); detected.Kind != got {
		t.Errorf("Detect() = %q but the kernel says %q", detected.Kind, got)
	}
}
