package server

// APIVersion is the wire contract between this bridge and the app, as a number
// that changes only when the contract does.
//
// The marketing version is not usable for this. `0.1.0` says nothing about whether a
// given request body is still accepted, and the two sides are updated by different
// people at different times — the bridge on the user's computer, the app whenever
// they get around to it. So there are two numbers: the version people read, and this
// one, which exists to be compared.
//
// **Bump it when a change would make an older counterpart fail**, and say which side
// has to move in the release notes. A change that only *adds* a response field does
// not need a bump: the app ignores unknown keys deliberately. A change to a *request*
// body does, because this bridge rejects unknown fields outright (see
// DisallowUnknownFields in the handlers) — that strictness is deliberate, and it is
// what makes this number worth having.
//
//	1 — first contract: pair, snapshot, decisions, session, keys, stream, feedback.
//	    (Multi-host added `host` to requests without breaking older callers: the
//	     field is optional and an old bridge simply treats its absence as "local".)
//	2 — working folder: POST /v1/projects requires an existing, absolute `cwd` (a
//	    blank one is refused instead of meaning home), POST /v1/sessions inherits the
//	    project folder, and GET /v1/folders is new. An app at 1 and a bridge at 2
//	    cannot create a project correctly, so both halves move together.
const APIVersion = 2
