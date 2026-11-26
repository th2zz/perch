package server

import (
	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/queue"
)

// The response envelopes, as types rather than as map literals.
//
// They were `map[string]any{"mirror": m, "decisions": d}` until the app's models
// were given a shared contract: a map literal is invisible to every check there
// is — the compiler cannot see a renamed key, and reflection cannot enumerate
// one — so the only thing standing between a rename and a phone that silently
// shows an empty screen was somebody reading the diff. As structs they are
// type-checked at the call site and their field names are enumerable, which is
// what `api_shape_test.go` and the app's `ApiShapeContractTest` now compare.
//
// The error body is deliberately not here yet. It is written in about twenty
// places as a bare map, and turning it into a type is a change to the error path
// of every handler rather than a change to a response shape; it is worth doing,
// and it is worth doing on its own. The consequence of a renamed `fix` is also
// the visible kind — the user gets an error with no next step — rather than the
// silent kind this file exists to prevent.

// snapshotEnvelope answers GET /v1/snapshot.
type snapshotEnvelope struct {
	Mirror    *discover.Mirror `json:"mirror"`
	Decisions []queue.Decision `json:"decisions"`
}

// decisionsEnvelope answers GET /v1/decisions.
type decisionsEnvelope struct {
	Decisions []queue.Decision `json:"decisions"`
}

// valueEnvelope answers GET /v1/value.
type valueEnvelope struct {
	Week queue.ValueStats `json:"week"`
}

// healthEnvelope answers GET /v1/health.
type healthEnvelope struct {
	BridgeVersion string      `json:"bridge_version"`
	APIVersion    int         `json:"api_version"`
	Health        core.Health `json:"health"`
}

// keysEnvelope answers POST /v1/session/keys.
//
// The decision response needs no envelope here: core.DecideResult already is one,
// and the shape test reflects over it directly.
type keysEnvelope struct {
	Command []string `json:"equivalent_command"`
}
