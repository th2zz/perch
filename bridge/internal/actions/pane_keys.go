package actions

import "fmt"

// The escape hatch's key bar.
//
// This is the one place in the product where a keystroke is not one of the four
// decision classes, and the distinction is the point:
//
//   - A **decision** is the product acting on the user's behalf. It has to be
//     provably equivalent to one herdr command, so the four classes are closed and
//     a mapping that cannot be determined is refused rather than guessed.
//   - The **escape hatch** is the user operating their own terminal. FR-017
//     requires them to be able to read *and write* the session there, and SC-012
//     requires a key bar for it. A user who opens a session and presses Esc is
//     typing, not instructing us.
//
// The first version of this product refused even this, on the grounds that
// arbitrary keys would be a fifth class of write. That reasoning defeated the
// requirement it was meant to protect: 37 of the 61 blocked dialogs in the
// installed manifests are deliberately not driven, and every one of them is a
// dialog a key bar would settle. A refusal with no remedy is not caution, it is a
// dead end.
//
// What keeps it safe is scope, not prohibition:
//
//  1. Only from the escape hatch. No card, and no part of the decision path, can
//     reach this.
//  2. A fixed allow-list, below. It is the vocabulary of a terminal dialog —
//     navigation, submit, cancel — not a keyboard. Anything outside it is refused.
//  3. Bounded in number, so a bug cannot turn into a burst of input.

// TerminalKeys is the whole vocabulary of the key bar, in the order it is shown.
//
// Chosen so that the keys a dialog can ask for are covered and nothing else is:
// cancel, submit, the four directions, tab and reverse tab, space, the two letters
// a yes/no prompt uses, and interrupt. The order puts the two a person reaches for
// most first — Esc and Enter — so they are under a thumb rather than scrolled to.
var TerminalKeys = []string{
	"esc", "enter", "y", "n", "up", "down", "left", "right", "tab", "shift+tab", "space", "ctrl+c",
}

// MaxKeysPerRequest bounds one press. A dialog is answered with one or two keys;
// anything larger is a bug or an attempt to use this as an input pump.
const MaxKeysPerRequest = 4

var terminalKeySet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(TerminalKeys))
	for _, key := range TerminalKeys {
		set[key] = struct{}{}
	}
	return set
}()

// AllowedKey reports whether a key may be sent from the escape hatch.
func AllowedKey(key string) bool {
	_, ok := terminalKeySet[key]
	return ok
}

// ValidatePaneKeys checks a request from the escape hatch.
func ValidatePaneKeys(paneID string, keys []string) error {
	if paneID == "" {
		return fmt.Errorf("which session? a pane id is required")
	}
	if len(keys) == 0 {
		return fmt.Errorf("no keys to send")
	}
	if len(keys) > MaxKeysPerRequest {
		return fmt.Errorf("at most %d keys at a time", MaxKeysPerRequest)
	}
	for _, key := range keys {
		if !AllowedKey(key) {
			// Named, so the app's own bug is obvious rather than mysterious. The
			// allow-list is the contract; this is where it is enforced.
			return fmt.Errorf("%q is not a key the escape hatch may send", key)
		}
	}
	return nil
}
