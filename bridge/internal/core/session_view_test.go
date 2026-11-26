package core

import "testing"

func TestReadDepthNeverGivesLessForAskingMore(t *testing.T) {
	if got := readDepth(160); got != 160 {
		t.Errorf("the app's floor should pass through: readDepth(160) = %d, want 160", got)
	}
	if got := readDepth(1000); got != 1000 {
		t.Errorf("what the app asks for on open should pass through: readDepth(1000) = %d, want 1000", got)
	}
	// The old clamp would answer 160 here, which is the failure this test exists for.
	if got := readDepth(5000); got != 2000 {
		t.Errorf("an absurd request should hit the ceiling, not the floor: readDepth(5000) = %d, want 2000", got)
	}

	// Monotone across the range: no value may be answered with less than a smaller value is.
	previous := 0
	for _, asked := range []int{1, 2, 160, 161, 400, 401, 1000, 2000, 2001, 100000} {
		got := readDepth(asked)
		if got < previous {
			t.Fatalf("readDepth(%d) = %d, less than the previous answer %d", asked, got, previous)
		}
		previous = got
	}
}

// A caller that asks for nothing gets a default, and a negative is a caller bug rather than an
// instruction to read everything.
func TestReadDepthDefaultsInsteadOfGuessing(t *testing.T) {
	for _, asked := range []int{0, -1, -400} {
		if got := readDepth(asked); got != 160 {
			t.Errorf("readDepth(%d) = %d, want the 160-line default", asked, got)
		}
	}
}
