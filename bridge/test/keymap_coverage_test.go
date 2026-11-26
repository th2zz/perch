package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/actions"
)

// TestEveryBlockedRuleIsAccountedFor is the guard against platform drift.
//
// herdr ships agent detection manifests — one file per agent, each a list of
// rules that decide when a pane is "blocked". A new rule means a new dialog that
// can stop an agent, and the product's promise is that such a stop either has a
// working button or an explicit explanation. It must never be a card with no
// button and no reason.
//
// So: read the manifests actually installed on this machine, take every rule
// that can mark a pane blocked, and require the bridge to know about all of
// them. A new manifest that we have not handled fails this test rather than
// silently degrading a user's phone.
//
// It skips when no manifests are present (a machine without herdr), because the
// check is only meaningful where herdr actually is.
func TestEveryBlockedRuleIsAccountedFor(t *testing.T) {
	manifests := findAgentManifests(t)
	if len(manifests) == 0 {
		t.Skip("no herdr agent manifests on this machine")
	}

	type blockedRule struct {
		agent string
		rule  string
	}
	var blocked []blockedRule
	seen := map[blockedRule]bool{}

	for _, path := range manifests {
		agent := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		for _, rule := range parseBlockedRules(t, path) {
			entry := blockedRule{agent: agent, rule: rule}
			if seen[entry] {
				continue
			}
			seen[entry] = true
			blocked = append(blocked, entry)
		}
	}

	if len(blocked) == 0 {
		t.Fatal("manifests were found but no blocked rules were parsed; the parser is wrong")
	}

	var unaccounted []string
	mapped, refused := 0, 0
	for _, entry := range blocked {
		status, reason := actions.Coverage(entry.agent, entry.rule)
		switch status {
		case actions.RuleMapped:
			mapped++
		case actions.RuleRefused:
			refused++
			if reason == "" {
				t.Errorf("%s/%s is refused without a reason the user could read", entry.agent, entry.rule)
			}
		default:
			unaccounted = append(unaccounted, entry.agent+"/"+entry.rule)
		}
	}

	t.Logf(
		"agent manifests: %d files, %d blocked rules — %d mappable, %d refused, %d unaccounted",
		len(manifests), len(blocked), mapped, refused, len(unaccounted),
	)

	if len(unaccounted) > 0 {
		t.Errorf(
			"%d blocked dialog(s) reached the product with no decision either way.\n"+
				"Add a key sequence to internal/actions/keymap.go, or a refusal with a reason:\n  %s",
			len(unaccounted), strings.Join(unaccounted, "\n  "),
		)
	}
}

// findAgentManifests locates herdr's installed detection manifests. Both the
// per-user state location and an XDG location are tried, and a missing directory
// is not an error.
func findAgentManifests(t *testing.T) []string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	roots := []string{
		filepath.Join(home, ".local", "state", "herdr", "agent-detection"),
		filepath.Join(home, ".config", "herdr", "agent-detection"),
	}
	var out []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// A missing directory is the normal case on a machine without
				// herdr; anything else during the walk is equally uninteresting,
				// because finding no manifests simply skips the test.
				return nil //nolint:nilerr // see above
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".toml") {
				out = append(out, path)
			}
			return nil
		})
	}
	return out
}

// parseBlockedRules pulls the rule ids whose state is "blocked" out of a
// manifest. A hand-rolled scan is right here: the subset we need is a flat,
// stable shape (`[[rules]]` blocks with `id` and `state` keys), and pulling in a
// TOML parser for two keys would be the tail wagging the dog.
func parseBlockedRules(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest %s: %v", path, err)
	}

	var rules []string
	var currentID string
	inRules := false

	commit := func(state string) {
		if currentID != "" && state == "blocked" {
			rules = append(rules, currentID)
		}
		currentID = ""
	}

	state := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			commit(state)
			inRules = strings.HasPrefix(line, "[[rules]]")
			state = ""
			continue
		}
		if !inRules || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "id":
			currentID = value
		case "state":
			state = value
		}
	}
	commit(state)
	return rules
}
