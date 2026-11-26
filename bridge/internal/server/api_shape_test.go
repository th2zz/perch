package server

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/th2zz/perch/bridge/internal/core"
	"github.com/th2zz/perch/bridge/internal/discover"
	"github.com/th2zz/perch/bridge/internal/queue"
	"github.com/th2zz/perch/bridge/internal/records"
	"github.com/th2zz/perch/bridge/internal/testfixture"
)

// The JSON field names of everything the app decodes, in one file both halves
// read.
//
// This is the widest of the silent seams. The two programs are written in
// different languages, so nothing type-checks across the boundary: rename a tag
// here and the app does not error — every one of its models is defaulted on
// purpose (so a newer bridge degrades instead of crashing), which means the
// renamed field arrives as an empty string or an empty list and the screen shows
// a blank row. The project has already been bitten by exactly this: `pane.read`
// answers inside a wrapper, and decoding the wrapper into the inner type
// "succeeded", leaving every excerpt empty with no error and no log.
//
// Tests did not catch it because the live ones check *behaviour with whatever data
// is to hand*: a list that arrives empty makes `for … { assert }` vacuous, and the
// machine usually has nothing waiting. This test does not need data at all — it
// compares the field names the bridge declares with the ones the app declares,
// through reflection on one side and kotlinx descriptors on the other.
//
// The app's half asserts a **subset**: every name it reads must be a name the
// bridge sends. The bridge is allowed to send more (the app ignores what it does
// not use), and that is how the two sets legitimately differ today — the
// decision record carries `tab_id` and `updated_at` that no screen shows.
//
// Regenerate after an intentional change:
//
//	PERCH_UPDATE_API_FIXTURE=1 go test ./internal/server/ -run WireShape

const apiShapeFixtureRelPath = "contracts/api-shape.json"

// shapeEntry is one type, keyed by a logical name both languages agree on. The Go
// type name is recorded so the file can be read by a person without the test.
type shapeEntry struct {
	Name   string   `json:"name"`
	GoType string   `json:"go_type"`
	Fields []string `json:"fields"`
}

type apiShape struct {
	Why   string       `json:"why"`
	Types []shapeEntry `json:"types"`
}

// wireShape is the mapping. Adding a type here is how a newly decoded model joins
// the contract, and the app's test fails until its own list catches up.
func wireShape() apiShape {
	shapes := []struct {
		name   string
		sample any
	}{
		{"mirror", discover.Mirror{}},
		{"host", discover.Host{}},
		{"workspace", discover.PlacedWorkspace{}},
		{"tab", discover.PlacedTab{}},
		{"pane", discover.PlacedPane{}},
		{"agent", discover.PlacedAgent{}},
		{"decision", queue.Decision{}},
		{"event", core.Event{}},
		{"session", core.SessionView{}},
		{"value_stats", queue.ValueStats{}},
		{"decide_result", core.DecideResult{}},
		{"health", core.Health{}},
		{"health_item", core.HealthItem{}},
		{"snapshot_response", snapshotEnvelope{}},
		{"decisions_response", decisionsEnvelope{}},
		{"value_response", valueEnvelope{}},
		{"health_response", healthEnvelope{}},
		{"folder", core.FolderEntry{}},
		{"folders_response", core.FoldersView{}},
		{"keys_response", keysEnvelope{}},
		{"usage_facts", records.Facts{}},
		{"conversation", core.ConversationView{}},
		{"conversation_message", records.Message{}},
	}

	out := apiShape{
		Why: "Every JSON field name the bridge sends and the app decodes. The app asserts that each name it " +
			"reads is one the bridge sends; a rename on either side fails a test instead of leaving a blank row. " +
			"The error body ({error, fix}) is deliberately out of scope: it is written as a bare map in about " +
			"twenty handlers, so pinning it means introducing a writer for it first.",
	}
	for _, s := range shapes {
		out.Types = append(out.Types, shapeEntry{
			Name:   s.name,
			GoType: typeName(s.sample),
			Fields: jsonFieldNames(s.sample),
		})
	}
	sort.Slice(out.Types, func(i, j int) bool { return out.Types[i].Name < out.Types[j].Name })
	return out
}

// typeName renders "discover.Mirror" rather than "*discover.Mirror": the pointer
// is an artefact of passing a zero value.
func typeName(v any) string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.PkgPath()[strings.LastIndex(t.PkgPath(), "/")+1:] + "." + t.Name()
}

// jsonFieldNames lists the names this type puts on the wire, sorted.
//
// It reads the same tags encoding/json reads, which is the point: it cannot drift
// from what the server actually sends. Embedded structs are flattened the way
// encoding/json flattens them; unexported fields and `json:"-"` are skipped
// because they never reach the wire.
func jsonFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var names []string
	var walk func(rt reflect.Type)
	walk = func(rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" && !f.Anonymous {
				continue // unexported
			}
			if f.Anonymous && f.Tag.Get("json") == "" {
				walk(f.Type) // embedded without a tag: flattened
				continue
			}
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			names = append(names, name)
		}
	}
	walk(t)
	sort.Strings(names)
	return names
}

func TestWireShapeMatchesTheSharedContract(t *testing.T) {
	current := wireShape()

	if os.Getenv("PERCH_UPDATE_API_FIXTURE") == "1" {
		testfixture.WriteJSON(t, apiShapeFixtureRelPath, current)
		return
	}

	var stored apiShape
	testfixture.ReadJSON(t, apiShapeFixtureRelPath, &stored)

	if len(stored.Types) == 0 {
		t.Fatalf("%s lists no types; the contract would be empty and this test vacuous", apiShapeFixtureRelPath)
	}
	if len(stored.Types) != len(current.Types) {
		t.Fatalf("the contract lists %d types and the bridge declares %d; "+
			"regenerate with PERCH_UPDATE_API_FIXTURE=1", len(stored.Types), len(current.Types))
	}

	byName := map[string]shapeEntry{}
	for _, e := range stored.Types {
		byName[e.Name] = e
	}
	for _, want := range current.Types {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("the contract has no entry for %q; regenerate with PERCH_UPDATE_API_FIXTURE=1", want.Name)
			continue
		}
		if !equalStrings(got.Fields, want.Fields) {
			// The message names the difference rather than dumping two lists:
			// whoever reads it wants to know which name moved.
			added := difference(want.Fields, got.Fields)
			removed := difference(got.Fields, want.Fields)
			t.Errorf("%s changed on the wire: added %v, removed %v\n"+
				"a renamed field arrives at the app as an empty value, not an error; "+
				"regenerate with PERCH_UPDATE_API_FIXTURE=1 and check the app still reads it",
				want.Name, added, removed)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// difference is in(a) but not in(b).
func difference(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}
