package herdr

import "testing"

// TestEveryExitWordIsALifecycleEvent: an event kind travels with one of herdr's two
// spellings depending on the door it came through (see names.go), and the exit story
// was missing from both.
//
// Measured on the #124 triage (2026-10-17, scratch session): typing `exit` in a pane
// emits pane_exited on the subscription stream — underscored, like every delivered
// kind — while the schema names the subscription pane.exited. Either spelling
// unanswered means the reconciler never re-reads the pane list, and the pane stays on
// the phone until the slow reconcile ticker. Callers classify with this predicate so
// they never spell either form themselves.
func TestEveryExitWordIsALifecycleEvent(t *testing.T) {
	for _, kind := range []string{EventPaneExited, EventPaneExitedHook} {
		if !LifecycleEvent(kind) {
			t.Errorf("LifecycleEvent(%q) = false; the reconciler would wait for the ticker instead of re-reading", kind)
		}
	}

	// The other lifecycle words keep working, in both spellings.
	for _, kind := range []string{
		EventPaneCreated, EventPaneCreatedHook,
		EventPaneClosed, EventPaneClosedHook,
		EventPaneAgentDetected, EventPaneAgentDetectedHook,
	} {
		if !LifecycleEvent(kind) {
			t.Errorf("LifecycleEvent(%q) = false", kind)
		}
	}

	// A status change has its own path; classifying it as lifecycle would turn every
	// status update into a full re-read.
	if LifecycleEvent(EventPaneAgentStatusChanged) {
		t.Errorf("a status change was classified as lifecycle")
	}
}
