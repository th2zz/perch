package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func base() Fingerprint {
	return Fingerprint{
		HerdrConfig:         FileStamp{Path: "/h/config.toml", Exists: true, SHA256: "aaa", Size: 10},
		HerdrPlugins:        FileStamp{Path: "/h/plugins.json", Exists: true, SHA256: "bbb", Size: 2},
		PluginsDir:          []string{"config"},
		OurPluginRegistered: false,
	}
}

// TestAnUntouchedMachineReportsNothing is the baseline: two readings of the same
// state must be silent, otherwise the check would cry wolf and get ignored.
func TestAnUntouchedMachineReportsNothing(t *testing.T) {
	if changes := Compare(base(), base()); len(changes) != 0 {
		t.Fatalf("expected no changes, got %+v", changes)
	}
}

// TestEditingTheUsersConfigIsTheFailureThisExistsFor.
func TestEditingTheUsersConfigIsTheFailureThisExistsFor(t *testing.T) {
	after := base()
	after.HerdrConfig.SHA256 = "ccc"

	changes := Compare(base(), after)
	if len(Unexpected(changes)) != 1 {
		t.Fatalf("editing config.toml must be reported as unexpected: %+v", changes)
	}
	if changes[0].Field != "herdr_config" {
		t.Errorf("wrong field blamed: %+v", changes[0])
	}
}

// TestRegisteringThePluginIsAllowedAndNamed covers the one change installing is
// supposed to cause.
func TestRegisteringThePluginIsAllowedAndNamed(t *testing.T) {
	after := base()
	after.OurPluginRegistered = true
	after.HerdrPlugins.SHA256 = "ddd"
	after.PluginsDir = append(after.PluginsDir, PluginID)

	changes := Compare(base(), after)
	if len(Unexpected(changes)) != 0 {
		t.Fatalf("installing must not be reported as a violation: %+v", changes)
	}
	if len(changes) != 2 {
		t.Fatalf("both the registration and the directory should be named: %+v", changes)
	}
	for _, change := range changes {
		if change.Kind != ChangeExpected {
			t.Errorf("change should be expected, got %+v", change)
		}
	}
}

// TestUnregisteringIsAlsoAllowedAndNamed: the same check has to hold on the way
// out, which is what makes "uninstall leaves nothing" testable.
func TestUnregisteringIsAlsoAllowedAndNamed(t *testing.T) {
	before := base()
	before.OurPluginRegistered = true
	before.HerdrPlugins.SHA256 = "ddd"
	before.PluginsDir = append(before.PluginsDir, PluginID)

	changes := Compare(before, base())
	if len(Unexpected(changes)) != 0 {
		t.Fatalf("uninstalling must not be reported as a violation: %+v", changes)
	}
	if len(changes) == 0 {
		t.Fatal("uninstalling should be noted")
	}
}

// TestAnotherPluginsChangeIsNotWavedThrough is the subtle one: the plugin
// registry is a file we may cause to change, which would be an easy place to stop
// looking. A change there that is not our own registration is still a failure.
func TestAnotherPluginsChangeIsNotWavedThrough(t *testing.T) {
	before := base()
	after := base()
	after.HerdrPlugins.SHA256 = "eee" // changed, but our registration did not move

	changes := Compare(before, after)
	if len(Unexpected(changes)) != 1 {
		t.Fatalf("an unrelated registry change must be reported: %+v", changes)
	}
	if changes[0].Field != "herdr_plugins" {
		t.Errorf("wrong field blamed: %+v", changes[0])
	}
}

// TestSomebodyElsesPluginDirectoryIsNoticed.
func TestSomebodyElsesPluginDirectoryIsNoticed(t *testing.T) {
	after := base()
	after.PluginsDir = append(after.PluginsDir, "someone.else")

	changes := Compare(base(), after)
	if len(Unexpected(changes)) != 1 {
		t.Fatalf("a foreign plugin directory must be reported: %+v", changes)
	}
}

// TestDeletingTheConfigIsAlsoAChange: "exists" is part of the identity.
func TestDeletingTheConfigIsAlsoAChange(t *testing.T) {
	after := base()
	after.HerdrConfig.Exists = false
	after.HerdrConfig.SHA256 = ""

	if len(Unexpected(Compare(base(), after))) != 1 {
		t.Fatal("a vanished config.toml must be reported")
	}
}

// TestTakeAgainstARealDirectoryTree is the end-to-end shape of the check, run
// against a synthetic home so it behaves the same on a machine without herdr.
func TestTakeAgainstARealDirectoryTree(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")

	before, err := Take(home, stateDir)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if before.HerdrConfig.Exists {
		t.Error("a fresh home directory has no herdr config, and the fingerprint should say so")
	}
	if before.StateDirExists {
		t.Error("the state directory does not exist yet")
	}
	if before.OurPluginRegistered {
		t.Error("nothing should be registered yet")
	}

	configDir := filepath.Join(home, ".config", "herdr")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("theme = dark\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "plugins.json"), []byte("[\""+PluginID+"\"]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	after, err := Take(home, stateDir)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !after.HerdrConfig.Exists {
		t.Error("the config should now exist")
	}
	if !after.OurPluginRegistered {
		t.Error("the plugin entry should be detected in plugins.json")
	}
	if !after.StateDirExists {
		t.Error("the state directory should be detected")
	}
	// A config file appearing where there was none is exactly what the check is
	// for, so it must be reported even though herdr itself would be the likely
	// author in real life.
	if len(Unexpected(Compare(before, after))) == 0 {
		t.Error("a config file appearing out of nowhere must be reported")
	}

	path := filepath.Join(t.TempDir(), "fp.json")
	if err := Write(path, after); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(Compare(after, reloaded)) != 0 {
		t.Error("a fingerprint must survive being written and read back")
	}
}
