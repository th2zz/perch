package core

import (
	"testing"

	"github.com/th2zz/perch/bridge/internal/state"
)

func TestSessionMapSurvivesReloadAndRejectsANewTitle(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	store, err := state.Open()
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	mapOne := loadSessionMap(store)
	mapOne.save(
		"w1:p1", "01a147f7-8415-7f20-80b2-ff97735ddd1d", 123,
		"Checkout perch readable=true | workspace", "/home/user/workspace",
	)

	mapTwo := loadSessionMap(store)
	if id, ok := mapTwo.lookup(
		"w1:p1", 999, "Checkout perch readable=true | workspace",
	); !ok || id != "01a147f7-8415-7f20-80b2-ff97735ddd1d" {
		t.Errorf("after reload, lookup = %q (ok=%v), want the saved session", id, ok)
	}
	if _, ok := mapTwo.lookup("w1:p1", 999, "Some other session | workspace"); ok {
		t.Error("a changed title reused the old session")
	}
}
