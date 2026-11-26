package core

import (
	"os"
	"sort"
	"testing"

	"github.com/th2zz/perch/bridge/internal/actions"
	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/herdr"
	"github.com/th2zz/perch/bridge/internal/queue"
	"github.com/th2zz/perch/bridge/internal/records"
	"github.com/th2zz/perch/bridge/internal/testfixture"
)


const vocabularyFixtureRelPath = "contracts/wire-vocabulary.json"

type vocabulary struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

type vocabularyFile struct {
	Why          string       `json:"why"`
	Vocabularies []vocabulary `json:"vocabularies"`
}

// wireVocabulary reads the values out of the typed constants rather than
// repeating the literals, so it cannot disagree with what the bridge sends.
func wireVocabulary() vocabularyFile {
	decisionStates := []string{
		string(queue.StatePending),
		string(queue.StateHandledLocally),
		string(queue.StateHandledElsewhere),
		string(queue.StateExpired),
	}
	allStatuses := herdr.AllAgentStatuses()
	agentStatuses := make([]string, len(allStatuses))
	needsDecisionStatuses := []string{}
	for i, s := range allStatuses {
		agentStatuses[i] = string(s)
		if s.NeedsDecision() {
			needsDecisionStatuses = append(needsDecisionStatuses, string(s))
		}
	}
	hostStatuses := []string{
		discover.StatusReachable,
		discover.StatusUnreachable,
		discover.StatusUnknown,
	}
	kinds := make([]string, 0, len(actions.AllKinds))
	for _, k := range actions.AllKinds {
		kinds = append(kinds, string(k))
	}

	return vocabularyFile{
		Why: "The closed value sets on the wire. The app compares against these strings " +
			"(no enum, so a future herdr state stays representable), and a rename makes a list " +
			"come back empty rather than raise an error — a quiet phone while agents wait. " +
			"Both sides read this file.",
		Vocabularies: []vocabulary{
			{Name: "decision_state", Values: decisionStates},
			{Name: "agent_status", Values: agentStatuses},
			{Name: "needs_decision_agent_status", Values: needsDecisionStatuses},
			{Name: "host_status", Values: hostStatuses},
			{Name: "decision_kind", Values: kinds},
			// The reading face's two speakers. Compared as strings by the app, which is why they are
			// here: renamed, every message would look like the agent's and nobody would see an error.
			{Name: "message_role", Values: []string{records.RoleYou, records.RoleAgent}},
		},
	}
}

func TestWireVocabularyMatchesTheSharedContract(t *testing.T) {
	current := wireVocabulary()

	if os.Getenv("PERCH_UPDATE_VOCABULARY_FIXTURE") == "1" {
		testfixture.WriteJSON(t, vocabularyFixtureRelPath, current)
		return
	}

	var stored vocabularyFile
	testfixture.ReadJSON(t, vocabularyFixtureRelPath, &stored)

	if len(stored.Vocabularies) == 0 {
		t.Fatalf("%s lists no vocabularies; the contract would be empty and this test vacuous",
			vocabularyFixtureRelPath)
	}

	byName := map[string][]string{}
	for _, v := range stored.Vocabularies {
		byName[v.Name] = v.Values
	}
	currentByName := map[string]bool{}
	for _, want := range current.Vocabularies {
		currentByName[want.Name] = true
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("the contract has no entry for %q; regenerate with PERCH_UPDATE_VOCABULARY_FIXTURE=1", want.Name)
			continue
		}
		if !sameSet(got, want.Values) {
			t.Errorf("%s changed on the wire: the bridge now says %v and the contract says %v\n"+
				"the app compares against these strings and a rename makes its list come back empty; "+
				"regenerate with PERCH_UPDATE_VOCABULARY_FIXTURE=1 and update the app too",
				want.Name, want.Values, got)
		}
	}
	for name := range byName {
		if !currentByName[name] {
			t.Errorf("stored contract contains %q which the bridge no longer produces; remove it or regenerate with PERCH_UPDATE_VOCABULARY_FIXTURE=1", name)
		}
	}
}

// sameSet ignores order: these are sets of names, not sequences, and a
// reordering is not a change to the contract.
func sameSet(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
