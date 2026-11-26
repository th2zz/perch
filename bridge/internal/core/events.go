package core

const (
	// EventSnapshot says "the whole world may have moved; re-read".
	EventSnapshot = "snapshot"
	// EventDecision says a new request is waiting for a person.
	EventDecision = "decision"
	// EventResolved says a request is no longer waiting — answered here, answered
	// on the computer, or expired.
	EventResolved = "resolved"
	// EventResync says the bridge's view may have fallen behind (a herdr read
	// failed) and the app should re-read rather than trust what it has.
	EventResync = "resync"
	// EventHeartbeat is the transport's keepalive. It carries no meaning for the
	// queue; it exists so an idle connection is not mistaken for a dead one.
	EventHeartbeat = "heartbeat"
)

// AllEventTypes is every type the bridge can emit, in one place.
//
// It exists so the set is enumerable rather than implied by scattered literals:
// the contract test compares it against the shared file, and the app compares its
// own list against the same file.
func AllEventTypes() []string {
	return []string{EventSnapshot, EventDecision, EventResolved, EventResync, EventHeartbeat}
}
