package core

import (
	"strings"
	"testing"
)

// TestSessionMessagesAreWrittenForTheUser covers the lines the escape hatch puts
// where a key row would be.
//
// These are assembled inside the bridge, which is why they escaped the app's own
// vocabulary check: a rendered screenshot was the first thing to notice a card
// telling the user about "detection rules" and "key tables".
func TestSessionMessagesAreWrittenForTheUser(t *testing.T) {
	messages := map[string]string{
		"no agent in the session": SessionNoAgent,
		"not waiting on anyone":   SessionNotWaiting,
	}

	for name, message := range messages {
		if strings.TrimSpace(message) == "" {
			t.Errorf("%s: an empty line where an explanation belongs is worse than none", name)
			continue
		}
		lower := strings.ToLower(message)
		for _, word := range []string{"herdr", "socket", "manifest", "detection", "key table"} {
			if strings.Contains(lower, word) {
				t.Errorf("%s: %q uses %q, which is our word and not the user's", name, message, word)
			}
		}
	}
}
