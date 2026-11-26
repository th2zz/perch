package core

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/th2zz/perch/bridge/internal/testherdr"
)

// TestMain lets this package's tests stand in for herdr.
//
// The bridge reaches other machines by running the user's herdr binary, so the
// only way to test that path without a second computer is to be that binary. When
// the same test process is started with the shim environment set, it answers as
// herdr and exits; otherwise it runs the tests.
func TestMain(m *testing.M) {
	testherdr.MaybeActAsHerdr()
	os.Exit(m.Run())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
