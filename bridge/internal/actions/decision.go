// Package actions turns a user intent into exactly one herdr command.
//
// This package is the mechanical expression of constitution II (action
// equivalence) and IV (minimal write surface). Two properties are load-bearing
// and enforced by tests in bridge/test:
//
//  1. Every write the product can perform is one of the four kinds below, and
//     each maps to a single herdr command that a human could type by hand
//     (contracts/herdr-usage.md).
//  2. When the mapping cannot be determined, Map returns an error. It never
//     guesses.
package actions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// Kind is one of the four write classes. There is no fifth.
type Kind string

const (
	// KindApprove lets the blocked agent proceed.
	KindApprove Kind = "approve"
	// KindReject declines the agent's request.
	KindReject Kind = "reject"
	// KindAnswer replies to a question the agent asked.
	KindAnswer Kind = "answer"
	// KindPrompt delivers free text the user wrote.
	KindPrompt Kind = "prompt"
)

// AllKinds is the closed set, used for request validation.
var AllKinds = []Kind{KindApprove, KindReject, KindAnswer, KindPrompt}

// Valid reports whether k is one of the four classes.
func (k Kind) Valid() bool {
	for _, known := range AllKinds {
		if k == known {
			return true
		}
	}
	return false
}

// NeedsText reports whether the kind carries user-written text.
func (k Kind) NeedsText() bool { return k == KindAnswer || k == KindPrompt }

// Request is exactly the body the app may POST to /v1/decisions. The field set
// is deliberately closed: there is no field for workspace layout, renaming, or
// bulk operations (constitution IV).
type Request struct {
	DecisionID string `json:"decision_id"`
	Kind       Kind   `json:"kind"`
	// Target is a herdr agent target or pane id, depending on the observation.
	Target string `json:"target"`
	// Text is required for answer/prompt and rejected for approve/reject.
	Text string `json:"text,omitempty"`
	// Host names the machine the target lives on: empty or "local" means the
	// machine the bridge runs on, anything else is a label from
	// `herdr machine list` (spec FR-016).
	//
	// It is part of the request because pane ids are only unique within one
	// machine: acting on the wrong computer is the one mistake this feature must
	// not make, so the host travels with the target rather than being inferred.
	Host string `json:"host,omitempty"`
}

// Validate rejects anything outside the four classes.
func (r Request) Validate() error {
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownKind, r.Kind)
	}
	if strings.TrimSpace(r.Target) == "" {
		return ErrMissingTarget
	}
	if r.Kind.NeedsText() && r.Text == "" {
		return fmt.Errorf("%w: %s requires text", ErrMissingText, r.Kind)
	}
	if !r.Kind.NeedsText() && r.Text != "" {
		return fmt.Errorf("%w: %s must not carry text", ErrUnexpectedText, r.Kind)
	}
	return nil
}

// ValidateWithoutDecision is the looser shape the escape hatch uses.
//
// A decision is what makes approve and reject meaningful — they answer a request
// that the computer raised. Delivering text is different: it is the user typing
// into their own session, which needs no request to exist. So the escape hatch may
// omit the request id, but only for the two text kinds, and everything else about
// the write is unchanged.
func (r Request) ValidateWithoutDecision() error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Kind != KindAnswer && r.Kind != KindPrompt {
		return fmt.Errorf(
			"%w: %s needs a request to act on; only answering and delivering text can stand alone",
			ErrUnknownKind, r.Kind,
		)
	}
	return nil
}

// Observation is what the bridge knows about the target when the user acts.
//
// The critical field is Rule: it is herdr's own detection verdict, obtained
// from `herdr agent explain`, naming the on-screen interaction. Guessing the
// keys from raw terminal text would be a heuristic; the rule id is evidence.
type Observation struct {
	// AgentKind is herdr's agent kind ("codex", "claude", "agy", ...).
	AgentKind string
	// AgentStatus is herdr's classification of the pane.
	AgentStatus herdr.AgentStatus
	// Rule is the detection rule id from `herdr agent explain`, e.g.
	// "bash_permission_prompt".
	Rule string
	// PromptText is the dialog excerpt, when the caller has one. It is only
	// consulted where the selected option changes what a key does: Claude's
	// folder-trust question puts the cautious option first in the home folder,
	// so Enter there means "No, exit". See LookupKeysForPrompt.
	PromptText string
	// RecognisedAgent is false when herdr sees a plain shell in this pane, in
	// which case only KindPrompt can be served (via pane.send_text).
	RecognisedAgent bool
}

// Command is the single herdr operation a decision resolves to.
type Command struct {
	Kind   Kind
	Method string
	Target string
	Keys   []string
	Text   string
	// CLI is the equivalent hand-runnable command. It exists so the
	// equivalence claim is testable and so the app can show the user exactly
	// what will happen.
	CLI []string
	// Remote is the same work as an executable argument list, for the case where
	// the target is another machine (spec FR-016).
	//
	// It is separate from CLI because CLI is a *sentence* — it may join two
	// commands with "&&" so the user can read what a tap does — and a sentence is
	// not something to hand to exec. Each entry here is one argv, run in order,
	// with the machine supplied by the caller. A test pins the two together: the
	// first entry must reproduce the first command CLI shows.
	Remote [][]string
}

// Errors. Each one is user-explainable and paired with a next step.
var (
	// ErrUnknownKind means the kind is outside the four classes.
	ErrUnknownKind = errors.New("unsupported decision kind")
	// ErrMissingTarget means no target was supplied.
	ErrMissingTarget = errors.New("decision has no target")
	// ErrMissingText means answer/prompt arrived without text.
	ErrMissingText = errors.New("decision requires text")
	// ErrUnexpectedText means approve/reject arrived with text attached.
	ErrUnexpectedText = errors.New("decision must not carry text")
	// ErrTargetMismatch means the request named a different session than the request
	// it belongs to. The queue is the authority on that address, so a disagreement is
	// refused rather than resolved in favour of whichever arrived with the action.
	ErrTargetMismatch = errors.New("this action names the wrong session")
	// ErrNoDeterministicKeys is the honest failure: herdr reported a blocked
	// agent but the on-screen interaction is not one we can drive with a
	// known key sequence. The user is told to open the escape hatch instead.
	ErrNoDeterministicKeys = errors.New("no deterministic key sequence for this prompt")
	// ErrNoAgent means answer was requested for a pane herdr does not see an
	// agent in.
	ErrNoAgent = errors.New("no agent detected in this session")
)
