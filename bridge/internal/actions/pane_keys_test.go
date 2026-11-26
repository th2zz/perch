package actions

import (
	"strings"
	"testing"
)

// TestTheKeyBarVocabularyIsExactlyWhatWeChose.
//
// This list is a widening of the write surface, so it is worth a test that fails
// when someone adds a key without thinking. The bar is for answering a dialog, not
// for typing: if this list ever grows towards a keyboard, that is a design change
// and it should have to argue with a red test rather than slip in.
func TestTheKeyBarVocabularyIsExactlyWhatWeChose(t *testing.T) {
	want := []string{
		"esc", "enter", "y", "n", "up", "down", "left", "right", "tab", "shift+tab", "space", "ctrl+c",
	}
	if len(TerminalKeys) != len(want) {
		t.Fatalf("key bar vocabulary changed: %v", TerminalKeys)
	}
	for i := range want {
		if TerminalKeys[i] != want[i] {
			t.Fatalf("key bar vocabulary changed: %v", TerminalKeys)
		}
	}
	// The two a person reaches for most come first, because the bar is one line
	// and the far end of it is a scroll away.
	if TerminalKeys[0] != "esc" || TerminalKeys[1] != "enter" {
		t.Errorf("esc and enter should lead: %v", TerminalKeys[:2])
	}
}

func TestOnlyTheListedKeysMayBeSent(t *testing.T) {
	for _, key := range TerminalKeys {
		if err := ValidatePaneKeys("wA:p1", []string{key}); err != nil {
			t.Errorf("%q should be allowed: %v", key, err)
		}
	}

	// Anything that would make this a general input channel.
	for _, key := range []string{
		"a", "q", "delete", "backspace", "ctrl+d", "f5", "prefix+x", "", "ENTER", "esc esc",
		"rm -rf /", "\\x1b",
	} {
		err := ValidatePaneKeys("wA:p1", []string{key})
		if err == nil {
			t.Errorf("%q must be refused", key)
			continue
		}
		if !strings.Contains(err.Error(), "not a key") {
			t.Errorf("%q produced an unhelpful error: %v", key, err)
		}
	}
}

func TestARequestIsBounded(t *testing.T) {
	if err := ValidatePaneKeys("", []string{"enter"}); err == nil {
		t.Error("a request without a pane must be refused")
	}
	if err := ValidatePaneKeys("wA:p1", nil); err == nil {
		t.Error("a request with no keys must be refused")
	}
	tooMany := []string{"enter", "enter", "enter", "enter", "enter"}
	if err := ValidatePaneKeys("wA:p1", tooMany); err == nil {
		t.Error("an unbounded number of keys must be refused")
	}
	// A dialog answer is one or two keys; four is the ceiling, not the norm.
	if err := ValidatePaneKeys("wA:p1", []string{"down", "enter"}); err != nil {
		t.Errorf("navigating and submitting should be allowed: %v", err)
	}
}
