package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/core"
)

func TestFoldersListsOnlyFoldersAndSupportsNavigation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, dir := range []string{"project-a", "project-b", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	ts, _ := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/v1/folders?path="+url.QueryEscape(root), testToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var view core.FoldersView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode folders: %v", err)
	}
	_ = resp.Body.Close()

	if view.Path != root || view.Home != root {
		t.Errorf("path/home = %q/%q, want %q", view.Path, view.Home, root)
	}
	if view.Parent != filepath.Dir(root) {
		t.Errorf("parent = %q, want %q", view.Parent, filepath.Dir(root))
	}
	if len(view.Entries) != 2 ||
		view.Entries[0].Name != "project-a" || view.Entries[1].Name != "project-b" {
		t.Errorf("entries = %+v, want project-a and project-b only", view.Entries)
	}
	for _, entry := range view.Entries {
		if entry.Path != filepath.Join(root, entry.Name) {
			t.Errorf("entry %q path = %q, want it inside %q", entry.Name, entry.Path, root)
		}
	}

	child := filepath.Join(root, "project-a")
	resp = do(t, ts, http.MethodGet, "/v1/folders?path="+url.QueryEscape(child), testToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("child status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode child folders: %v", err)
	}
	_ = resp.Body.Close()
	if view.Path != child || view.Parent != root {
		t.Errorf("child path/parent = %q/%q, want %q/%q", view.Path, view.Parent, child, root)
	}
}

func TestFoldersStartsAtHomeWhenNoFolderIsNamed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	ts, _ := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/v1/folders", testToken, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()
	var view core.FoldersView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode folders: %v", err)
	}
	if view.Path != root || view.Home != root {
		t.Errorf("path/home = %q/%q, want the home folder %q", view.Path, view.Home, root)
	}
}

func TestFoldersRejectsBadPathsWithAFix(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	ts, _ := newTestServer(t)

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"missing", filepath.Join(root, "gone"), "does not exist"},
		{"file", file, "not a folder"},
		{"relative", "somewhere", "full folder path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := call(t, ts, http.MethodGet,
				"/v1/folders?path="+url.QueryEscape(tc.path), testToken, "")
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", status)
			}
			if body["error"] == "" || body["fix"] == "" {
				t.Errorf("body = %v, want a reason and a next step", body)
			}
			if !strings.Contains(body["error"], tc.want) {
				t.Errorf("error = %q, want it to name %q", body["error"], tc.want)
			}
		})
	}
}
