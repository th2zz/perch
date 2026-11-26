package main

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests cover the only destructive code in the product. A bug here would
// not be an inconvenience, so the guards are tested as behaviour rather than
// trusted as comments.

func TestRemoveStateDirRefusesPathsItCannotJustify(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot resolve home: %v", err)
	}

	cases := []struct {
		name string
		dir  stateDir
	}{
		{"a relative path", stateDir{path: "state"}},
		{"nothing at all", stateDir{path: ""}},
		{"the filesystem root", stateDir{path: string(filepath.Separator)}},
		{"the home directory", stateDir{path: home, owned: true}},
		{"a parent of home", stateDir{path: filepath.Dir(home)}},
		{"a top-level directory", stateDir{path: filepath.Join(string(filepath.Separator), "state")}},
		// An owned path is one we computed, so it must carry our name; anything
		// else means the resolution went wrong and the safe move is to stop.
		{"an owned path with someone else's name", stateDir{
			path:  filepath.Join(home, ".config", "something-else"),
			owned: true,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			removed, err := removeStateDir(tc.dir)
			if err == nil {
				t.Fatalf("expected a refusal, got removed=%v", removed)
			}
			if removed {
				t.Fatal("nothing should have been removed")
			}
		})
	}
}

func TestRemoveStateDirRemovesWhatItShould(t *testing.T) {
	base := t.TempDir()

	// A directory herdr injected: not named after the plugin, and legitimate.
	injected := filepath.Join(base, "state")
	if err := os.MkdirAll(injected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(injected, "pairing.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := removeStateDir(stateDir{path: injected})
	if err != nil {
		t.Fatalf("an injected state directory should be removable: %v", err)
	}
	if !removed {
		t.Fatal("it existed and should have been removed")
	}
	if _, err := os.Stat(injected); !os.IsNotExist(err) {
		t.Fatal("the directory is still there")
	}

	// One of our own paths, named as we name them.
	ours := filepath.Join(base, "perch-bridge")
	if err := os.MkdirAll(ours, 0o700); err != nil {
		t.Fatal(err)
	}
	removed, err = removeStateDir(stateDir{path: ours, owned: true})
	if err != nil || !removed {
		t.Fatalf("our own directory should be removable: removed=%v err=%v", removed, err)
	}

	// Something that is already gone is not an error: uninstalling twice is a
	// reasonable thing for a person to do.
	removed, err = removeStateDir(stateDir{path: filepath.Join(base, "never-existed")})
	if err != nil {
		t.Fatalf("a missing directory is not a failure: %v", err)
	}
	if removed {
		t.Fatal("nothing was there to remove")
	}
}
