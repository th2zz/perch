package actions

import (
	"strings"
	"testing"
)

// TestReasonsAreWrittenForTheUser is the counterpart, on this side, of the app's
// copy test.
//
// Every one of these strings is displayed verbatim: on a decision card, and in the
// escape hatch. They are assembled inside the bridge, which is exactly why they
// escaped the app's vocabulary check — and why a rendered screenshot saying
// "detection rule some_unmapped_dialog is not in the bridge's key table" was the
// first anyone noticed. The user does not have a bridge, a key table, or detection
// rules; they have a computer that is asking them something.
//
// The banned words are the ones that only make sense to whoever built this.
func TestReasonsAreWrittenForTheUser(t *testing.T) {
	banned := []string{
		"herdr", "manifest", "key table", "detection rule", "regex", "priority",
		"pane title", "rule id", "regexp", "schema", "socket",
	}

	reasons := map[string]string{
		"no rule named":  reasonNoRule,
		"unknown dialog": reasonUnknownDialog,
	}
	for rule, reason := range RefusedRules() {
		reasons[rule] = reason
	}

	if len(reasons) < 20 {
		t.Fatalf("only %d reasons found; the test is not reading the table", len(reasons))
	}

	for name, reason := range reasons {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s: a refusal with no explanation leaves the user with nothing to do", name)
			continue
		}
		lower := strings.ToLower(reason)
		for _, word := range banned {
			if strings.Contains(lower, word) {
				t.Errorf("%s: %q tells the user about %q, which is our vocabulary, not theirs", name, reason, word)
			}
		}
		// What is deliberately *not* checked here: that each reason spells out what
		// to do next. The card and the escape hatch already put a next step beside
		// every reason, in one place. Repeating it in thirty strings is how a
		// product ends up with thirty slightly different instructions — and the
		// first version of this test failed two reasons for not repeating it.
	}
}
