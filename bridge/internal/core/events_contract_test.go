package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/testfixture"
)

// The event types are the vocabulary of the one live connection the product
// depends on, and the app switches on them by exact string with no fallback: an
// unrecognised type is ignored, so a rename on one side and not the other stops
// the phone being told that a person is needed — silently, in the optimistic
// direction. This test is the bridge's half of the agreement, and the app's half
// reads the same file.
//
// Regenerate after an intentional change (which is a breaking change to the
// contract: bump APIVersion too, or an installed app will silently ignore the new
// name):
//
//	PERCH_UPDATE_EVENTS_FIXTURE=1 go test ./internal/core/ -run EventTypes

const eventsFixtureRelPath = "contracts/bridge-events.json"

type eventsFixture struct {
	Why   string   `json:"why"`
	Types []string `json:"types"`
}

func TestEventTypesMatchTheSharedContract(t *testing.T) {
	if os.Getenv("PERCH_UPDATE_EVENTS_FIXTURE") == "1" {
		testfixture.WriteJSON(t, eventsFixtureRelPath, eventsFixture{
			Why: "Every type the bridge can emit on the event stream. Both sides read this file; " +
				"an unrecognised type is ignored by the app, so a one-sided rename is silent.",
			Types: AllEventTypes(),
		})
		return
	}

	var fixture eventsFixture
	testfixture.ReadJSON(t, eventsFixtureRelPath, &fixture)

	got := AllEventTypes()
	if len(got) == 0 {
		t.Fatal("AllEventTypes returned nothing; the contract would be empty and this test vacuous")
	}
	if !sameSet(got, fixture.Types) {
		t.Errorf("the bridge emits %v and the shared contract lists %v; "+
			"regenerate with PERCH_UPDATE_EVENTS_FIXTURE=1 and bump APIVersion, "+
			"because an installed app ignores a type it does not know", got, fixture.Types)
	}

	// The heartbeat is the one type that carries no meaning for the queue. The
	// app ignores it deliberately, so it must not quietly become a queue event.
	if !contains(fixture.Types, EventHeartbeat) {
		t.Errorf("%q is missing from the contract: the transport sends it and the app's decoder is tested against it", EventHeartbeat)
	}
}

// TestNoEventTypeIsWrittenAsALiteral is the tripwire for the refactor that made
// this file possible.
//
// Naming the constants fixes today's thirteen call sites; it does nothing about
// the fourteenth. `publish(Event{Type: "sdecision"})` — or a plausible-looking
// rename — would compile, pass every behavioural test (the app ignores what it
// does not know, and nothing else reads the type), and be invisible until a phone
// stopped updating. The check is a grep rather than a type-level guarantee
// because the field has to stay a string to be serialised; a grep that fails the
// build is the honest version of "we agreed not to do this".
func TestNoEventTypeIsWrittenAsALiteral(t *testing.T) {
	packages := []string{
		testfixture.RepoRoot(t) + "/bridge/internal/core",
		testfixture.RepoRoot(t) + "/bridge/internal/server",
	}

	for _, dir := range packages {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			// This file is the one place a literal is allowed to appear: it is
			// where the strings are defined.
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "events.go" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			for i, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				// Comments are how the constants are explained, and how the old
				// literal list is documented; only code counts.
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				// Any of the ways the field is written: in a composite literal, in
				// a map read by json.Marshal, or assigned. The field is only ever
				// the event type in these two packages.
				if strings.Contains(line, `Type: "`) ||
					strings.Contains(line, `Type = "`) ||
					strings.Contains(line, `"type": "`) {
					t.Errorf("%s:%d writes an event type as a literal: %s\n"+
						"use one of the Event* constants in events.go so the set stays enumerable",
						name, i+1, trimmed)
				}
			}
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
