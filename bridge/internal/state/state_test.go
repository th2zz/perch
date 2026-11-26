package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestNothingThisDirectoryHoldsIsReadableByAnyoneElse.
//
// The state directory holds the token that lets a phone act on the user's computer:
// approve an agent, type into a pane. On a shared or corporate machine — the exact
// setting this product is bought for — a world-readable file there is a stranger with
// the user's keyboard. It is a property that no test would notice losing: the feature
// works perfectly with the file at 0644, right up until somebody reads it.
func TestNothingThisDirectoryHoldsIsReadableByAnyoneElse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes do not describe Windows ACLs")
	}

	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv(EnvStateDir, dir)

	store, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := store.WriteJSON(FilePairing, map[string]string{"token": "not-a-real-token"}); err != nil {
		t.Fatalf("write pairing: %v", err)
	}
	if err := store.WriteJSON(FileDecisions, []string{"one"}); err != nil {
		t.Fatalf("write decisions: %v", err)
	}

	if mode := modeOf(t, store.Dir()); mode != 0o700 {
		t.Errorf("the state directory is %o, want 0700", mode)
	}
	for _, name := range []string{FilePairing, FileDecisions} {
		if mode := modeOf(t, store.Path(name)); mode != 0o600 {
			t.Errorf("%s is %o, want 0600", name, mode)
		}
	}
}

// TestWritingTwiceLeavesNoTemporaryFileBehind: writes go through a temporary file
// plus a rename, which is the right way to make them atomic — and exactly the way to
// leave a second copy of a secret on disk if the temporary is not cleaned up.
func TestWritingTwiceLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv(EnvStateDir, dir)

	store, err := Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, value := range []string{"first", "second"} {
		if err := store.WriteJSON(FilePairing, map[string]string{"token": value}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	entries, err := os.ReadDir(store.Dir())
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 1 || names[0] != FilePairing {
		t.Errorf("state directory holds %v, want only %s", names, FilePairing)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
