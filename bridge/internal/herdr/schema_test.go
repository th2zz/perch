package herdr

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)


type schemaDoc struct {
	Protocol      int `json:"protocol"`
	SchemaVersion int `json:"schema_version"`
	Schemas       struct {
		Request struct {
			OneOf []struct {
				Properties struct {
					Method struct {
						Const string `json:"const"`
					} `json:"method"`
					Params json.RawMessage `json:"params"`
				} `json:"properties"`
			} `json:"oneOf"`
			Defs map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"$defs"`
		} `json:"request"`
	} `json:"schemas"`
}

func loadSchema(t *testing.T) schemaDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "herdr-schema.json"))
	if err != nil {
		t.Fatalf("no schema snapshot: %v (regenerate with `herdr api schema --json > testdata/herdr-schema.json`)", err)
	}
	var doc schemaDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the schema snapshot is not readable JSON: %v", err)
	}
	return doc
}

// paramsOf finds the parameter schema for a method, following a `$ref` into `$defs` when there is one.
func paramsOf(doc schemaDoc, method string) (map[string]json.RawMessage, bool) {
	for _, entry := range doc.Schemas.Request.OneOf {
		if entry.Properties.Method.Const != method {
			continue
		}
		var ref struct {
			Ref string `json:"$ref"`
		}
		if err := json.Unmarshal(entry.Properties.Params, &ref); err == nil && ref.Ref != "" {
			name := ref.Ref[strings.LastIndex(ref.Ref, "/")+1:]
			def, ok := doc.Schemas.Request.Defs[name]
			if !ok {
				return nil, false
			}
			return def.Properties, true
		}
		var inline struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(entry.Properties.Params, &inline); err != nil {
			return nil, false
		}
		return inline.Properties, true
	}
	return nil, false
}

// theSurface is every method this bridge calls, with the parameters it sends.
//
// This list is the contract. Adding a call to the bridge without adding it here does not fail the build —
// the compiler cannot see inside a string — so the discipline is that this table and the `herdr` package's
// methods are edited together, and a reviewer can read the whole dependency surface in one place.
//
// Parameters are listed as the bridge actually sends them, optional ones included: `pane.read` is called
// with a source and a format every time, so all five names are the contract even though herdr only requires
// two. A parameter that is optional upstream and always sent by us is a parameter we break on.
//
// The CLI half of the surface (groups, verbs, flags in commands.go) is not held here, because herdr
// publishes no machine-readable description of it; `boundary_test.go` keeps those names in this package,
// which is what makes an upstream rename a one-file edit.
var theSurface = map[string][]string{
	"ping":              {},
	"session.snapshot":  {},
	"pane.get":          {"pane_id"},
	"pane.layout":       {"pane_id"},
	"pane.process_info": {"pane_id"},
	"pane.read":         {"pane_id", "source", "strip_ansi", "format", "lines"},
	"pane.send_keys":    {"pane_id", "keys"},
	"pane.send_text":    {"pane_id", "text"},
	"pane.send_input":   {"pane_id", "text", "keys"},
	"pane.rename":       {"pane_id", "label"},
	"pane.close":        {"pane_id"},
	"agent.read":        {"target", "source", "strip_ansi", "format", "lines"},
	"agent.send_keys":   {"target", "keys"},
	"agent.prompt":      {"target", "text"},
	"workspace.rename":  {"workspace_id", "label"},
	"workspace.close":   {"workspace_id"},
	"events.subscribe":  {"subscriptions"},
}

func TestEveryHerdrMethodWeCallStillExists(t *testing.T) {
	doc := loadSchema(t)
	if doc.Protocol == 0 {
		t.Fatal("the snapshot has no protocol number; it is not the file this test thinks it is")
	}
	t.Logf("snapshot speaks herdr protocol %d, schema version %d", doc.Protocol, doc.SchemaVersion)

	var missing []string
	for method := range theSurface {
		if _, ok := paramsOf(doc, method); !ok {
			missing = append(missing, method)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("herdr no longer offers %v — the bridge calls these; update the herdr package and this "+
			"table together, and check what replaced them", missing)
	}
}

func TestEveryParameterWeSendIsStillKnown(t *testing.T) {
	doc := loadSchema(t)

	for method, used := range theSurface {
		params, ok := paramsOf(doc, method)
		if !ok {
			continue // the test above reports the missing method
		}
		for _, name := range used {
			if _, known := params[name]; !known {
				known := make([]string, 0, len(params))
				for k := range params {
					known = append(known, k)
				}
				sort.Strings(known)
				t.Errorf("%s no longer takes %q; it takes %v — a renamed parameter is a silent failure at "+
					"runtime (the request is refused with a message nobody on the phone can act on)",
					method, name, known)
			}
		}
	}
}

// theEventSurface is every event type this bridge subscribes to. Like theSurface it is the contract:
// a renamed event is silent at compile time (the name is a string in a request) and at runtime (herdr
// either refuses the subscription or, worse, accepts a stale set and the stream simply goes quiet), so
// it is held to the snapshot here.
//
// pane.exited joined the list with issue #124: herdr announces "the process inside this pane ended"
// with it, and a bridge that stops asking would put panes back onto the five-minute backstop.
var theEventSurface = []string{
	EventPaneCreated,
	EventPaneClosed,
	EventPaneExited,
	EventPaneAgentDetected,
	EventPaneAgentStatusChanged,
}

// TestEveryEventWeSubscribeToStillExists reads the Subscription union out of the snapshot and checks
// each name above is still one of its arms.
func TestEveryEventWeSubscribeToStillExists(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "herdr-schema.json"))
	if err != nil {
		t.Fatalf("no schema snapshot: %v", err)
	}
	var doc struct {
		Schemas struct {
			Request struct {
				Defs map[string]json.RawMessage `json:"$defs"`
			} `json:"request"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the schema snapshot is not readable JSON: %v", err)
	}
	var subscription struct {
		OneOf []struct {
			Properties struct {
				Type struct {
					Const string `json:"const"`
				} `json:"type"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(doc.Schemas.Request.Defs["Subscription"], &subscription); err != nil {
		t.Fatalf("no Subscription union in the snapshot: %v", err)
	}
	known := map[string]bool{}
	for _, arm := range subscription.OneOf {
		known[arm.Properties.Type.Const] = true
	}
	if len(known) < 10 {
		t.Fatalf("the Subscription union has %d arms; the parser is reading the wrong shape", len(known))
	}
	var missing []string
	for _, want := range theEventSurface {
		if !known[want] {
			missing = append(missing, want)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("herdr no longer offers %v as subscriptions — that part of the stream would go quiet and "+
			"the reconciler would fall back to the slow periodic re-read. Update names.go and this list "+
			"together, and check what replaced them", missing)
	}
}

// A label is bounded by the product, not by herdr, and the bound has to hold for both kinds of name.
func TestLabelsAreBoundedBeforeTheyReachTheHost(t *testing.T) {
	if _, err := CleanLabel("  fix the parser  "); err != nil {
		t.Fatalf("a plain name was refused: %v", err)
	}
	if got, _ := CleanLabel("  fix the parser  "); got != "fix the parser" {
		t.Errorf("a name is not trimmed: %q", got)
	}
	for _, bad := range []string{"", "   ", "two\nlines", "tab\there", strings.Repeat("x", maxLabel+1)} {
		if _, err := CleanLabel(bad); err == nil {
			t.Errorf("%q was accepted as a name; it cannot be drawn in a list row", bad)
		}
	}
	// Exactly at the bound is allowed: the limit is a screen, not a mood.
	if _, err := CleanLabel(strings.Repeat("x", maxLabel)); err != nil {
		t.Errorf("a name of exactly %d characters was refused: %v", maxLabel, err)
	}
}

// declaredAgentStatusesFromSource parses types.go to extract all declared Status* constants.
func declaredAgentStatusesFromSource(t *testing.T) []AgentStatus {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatalf("cannot parse types.go: %v", err)
	}
	var declared []AgentStatus
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vspec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vspec.Names {
				if strings.HasPrefix(name.Name, "Status") {
					if len(vspec.Values) > 0 {
						if lit, ok := vspec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							val, err := strconv.Unquote(lit.Value)
							if err == nil {
								declared = append(declared, AgentStatus(val))
							}
						}
					}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no Status* constants found in types.go; parser is broken")
	}
	return declared
}

// TestHerdrAgentStatusValuesMatchSnapshot pins herdr's AgentStatus enum against our declaration (D55 ④).
// It verifies:
//  1. All declared constants in types.go (Status*) match AllAgentStatuses() using the compiler's own AST;
//  2. All schema sections (event, request, subscription_event, success_response) contain every status
//     we depend on. Upstream additions are allowed ("growth is free; breakage is not"), but any removal
//     or rename fails immediately.
func TestHerdrAgentStatusValuesMatchSnapshot(t *testing.T) {
	declared := declaredAgentStatusesFromSource(t)
	allStatuses := AllAgentStatuses()

	if len(allStatuses) != len(declared) {
		t.Fatalf("AllAgentStatuses() has %d entries but types.go declares %d Status* constants: %v",
			len(allStatuses), len(declared), declared)
	}
	declaredSet := map[AgentStatus]bool{}
	for _, d := range declared {
		declaredSet[d] = true
	}
	for _, s := range allStatuses {
		if !declaredSet[s] {
			t.Fatalf("AllAgentStatuses() contains %q which is not a declared Status* constant in types.go", s)
		}
	}

	raw, err := os.ReadFile(filepath.Join("testdata", "herdr-schema.json"))
	if err != nil {
		t.Fatalf("no schema snapshot: %v", err)
	}
	var doc struct {
		Schemas map[string]struct {
			Defs map[string]struct {
				Enum []string `json:"enum"`
			} `json:"$defs"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the schema snapshot is not readable JSON: %v", err)
	}

	want := make([]string, len(allStatuses))
	for i, s := range allStatuses {
		want[i] = string(s)
	}
	sort.Strings(want)

	sectionsChecked := 0
	for section, sdata := range doc.Schemas {
		statusDef, ok := sdata.Defs["AgentStatus"]
		if !ok {
			continue
		}
		gotSet := map[string]bool{}
		for _, e := range statusDef.Enum {
			gotSet[e] = true
		}
		var missing []string
		for _, required := range want {
			if !gotSet[required] {
				missing = append(missing, required)
			}
		}
		if len(missing) > 0 {
			t.Fatalf("herdr schema section %q missing required AgentStatus: %v (snapshot has %v)", section, missing, statusDef.Enum)
		}
		sectionsChecked++
	}
	if sectionsChecked < 4 {
		t.Fatalf("expected at least 4 schema sections with AgentStatus, checked %d", sectionsChecked)
	}
}

// TestOnlyBlockedNeedsDecision asserts that within AllAgentStatuses(), exactly and only
// StatusBlocked returns true for NeedsDecision().
func TestOnlyBlockedNeedsDecision(t *testing.T) {
	for _, s := range AllAgentStatuses() {
		if s == StatusBlocked {
			if !s.NeedsDecision() {
				t.Errorf("%s.NeedsDecision() = false, want true", s)
			}
		} else {
			if s.NeedsDecision() {
				t.Errorf("%s.NeedsDecision() = true, want false", s)
			}
		}
	}
}
