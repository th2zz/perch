// Package testfixture locates the shared contract files a test reads from the
// checkout.
//
// Some things both halves of the product must agree on live in a file rather than
// in code: the bytes of a pairing link, the names of the event types. A test in
// this module reads them, and a test in the Android module reads the same file.
// The path is found by walking up to the checkout rather than by a configured
// variable, so the tests work from any package and from any directory Gradle or
// `go test` happens to run them in.
//
// It deliberately does not skip when the file is missing. A contract test that
// quietly does nothing is worse than no test: it reports green for the one thing
// it was written to catch.
package testfixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is cached per process: the walk is the same for every test in a run.
var repoRoot string

// RepoRoot is the checkout the test is running in, found by the `.git` marker.
func RepoRoot(t testing.TB) string {
	t.Helper()
	if repoRoot != "" {
		return repoRoot
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			repoRoot = dir
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find the repository root above %s (no .git)", dir)
		}
		dir = parent
	}
}

// Path resolves a repo-relative path, failing the test if the file is not there.
func Path(t testing.TB, rel string) string {
	t.Helper()
	full := filepath.Join(RepoRoot(t), filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("contract file %s is missing (%v); it is shared with the app's tests and must exist in the checkout", rel, err)
	}
	return full
}

// ReadJSON decodes a repo-relative JSON file into out.
func ReadJSON(t testing.TB, rel string, out any) {
	t.Helper()
	raw, err := os.ReadFile(Path(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
}

// WriteJSON renders v as the indented, HTML-unescaped JSON a contract file is
// written in. HTML escaping off so that `&` reads as `&`: a person compares these
// files against links in a chat app, and both parsers accept either form.
func WriteJSON(t testing.TB, rel string, v any) {
	t.Helper()
	full := filepath.Join(RepoRoot(t), filepath.FromSlash(rel))
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		t.Fatalf("marshal %s: %v", rel, err)
	}
	if err := os.WriteFile(full, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	t.Logf("rewrote %s", rel)
}
