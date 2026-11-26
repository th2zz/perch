package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func TestListFoldersShowsOnlyFoldersSortedAndHidesDotDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	mustMkdir(t, filepath.Join(root, "beta"))
	mustMkdir(t, filepath.Join(root, "Alpha"))
	mustMkdir(t, filepath.Join(root, ".hidden"))
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "Alpha"), filepath.Join(root, "linked")); err != nil {
		t.Logf("symlinks unavailable, not checking one: %v", err)
	}

	view, err := listFolders(root)
	if err != nil {
		t.Fatalf("listFolders: %v", err)
	}
	if view.Path != root || view.Home != root {
		t.Errorf("path/home = %q/%q, want %q/%q", view.Path, view.Home, root, root)
	}
	if view.Parent != filepath.Dir(root) {
		t.Errorf("parent = %q, want %q", view.Parent, filepath.Dir(root))
	}
	if view.Truncated {
		t.Error("an ordinary directory was reported as truncated")
	}
	var names []string
	for _, entry := range view.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"Alpha", "beta"}
	if _, err := os.Lstat(filepath.Join(root, "linked")); err == nil {
		want = append(want, "linked")
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("entries = %v, want %v (only folders, case-insensitive, no dot-directories)", names, want)
	}
	for _, entry := range view.Entries {
		if !strings.HasPrefix(entry.Path, root) {
			t.Errorf("entry %q escaped the listed folder: %q", entry.Name, entry.Path)
		}
	}
}

func TestListFoldersStartsAtHomeAndExpandsTilde(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	mustMkdir(t, filepath.Join(root, "projects", "one"))

	for _, requested := range []string{"", "  ", "~", "~/"} {
		view, err := listFolders(requested)
		if err != nil {
			t.Fatalf("listFolders(%q): %v", requested, err)
		}
		if view.Path != root || view.Home != root {
			t.Errorf("listFolders(%q) path/home = %q/%q, want %q", requested, view.Path, view.Home, root)
		}
	}

	view, err := listFolders("~/projects")
	if err != nil {
		t.Fatalf("listFolders(~/projects): %v", err)
	}
	if view.Path != filepath.Join(root, "projects") || view.Parent != root {
		t.Errorf("path/parent = %q/%q, want %q/%q", view.Path, view.Parent, filepath.Join(root, "projects"), root)
	}
}

func TestListFoldersRejectsMissingFilesAndRelativePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cases := []struct {
		name      string
		requested string
		want      string
	}{
		{"missing", filepath.Join(root, "gone"), "does not exist"},
		{"file", file, "not a folder"},
		{"relative", "somewhere/else", "full folder path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := listFolders(tc.requested)
			if err == nil {
				t.Fatalf("listFolders(%q) succeeded, want an error", tc.requested)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestListFoldersReportsWhenItStoppedAtTheLimit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for i := 0; i <= folderEntryLimit; i++ {
		mustMkdir(t, filepath.Join(root, fmt.Sprintf("dir-%04d", i)))
	}

	view, err := listFolders(root)
	if err != nil {
		t.Fatalf("listFolders: %v", err)
	}
	if len(view.Entries) != folderEntryLimit {
		t.Errorf("entries = %d, want the %d-entry cap", len(view.Entries), folderEntryLimit)
	}
	if !view.Truncated {
		t.Error("a directory larger than the cap must say it was cut short")
	}
}

func TestAnAbsoluteFolderDoesNotNeedAHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", "")
	if got, err := resolveFolder(root); err != nil || got != root {
		t.Errorf("resolveFolder(%q) = %q, %v; an absolute folder needs no home", root, got, err)
	}
	if _, err := resolveFolder("~"); err == nil {
		t.Error("~ without a home folder should be refused")
	}
}

func TestHomeDirCleansTheEnvironmentSpelling(t *testing.T) {
	t.Setenv("HOME", "/home//me/.")
	if got := homeDir(); got != "/home/me" {
		t.Errorf("homeDir() = %q, want /home/me so the phone's home comparison holds", got)
	}
}

func TestProjectCwdRequiresAnExplicitExistingFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := projectCwd(""); err == nil || !strings.Contains(err.Error(), "working folder is required") {
		t.Errorf("blank cwd error = %v, want a request for an explicit working folder", err)
	}
	if _, err := projectCwd("relative"); err == nil || !strings.Contains(err.Error(), "full folder path") {
		t.Errorf("relative cwd error = %v, want the full-path rule", err)
	}
	if _, err := projectCwd(file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("file cwd error = %v, want the file refused", err)
	}
	got, err := projectCwd("~/")
	if err != nil {
		t.Fatalf("projectCwd(~/): %v", err)
	}
	if got != root {
		t.Errorf("projectCwd(~/)= %q, want %q", got, root)
	}
}
