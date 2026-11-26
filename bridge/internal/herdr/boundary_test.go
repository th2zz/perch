package herdr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)


// allowed holds the files that are permitted to speak herdr's language, with the reason. Keep it short: a
// file here is a file the boundary does not cover.
var allowed = map[string]string{
	"internal/testherdr/shim.go": "a fake herdr executable built for tests: it has to answer herdr's own " +
		"method names and flags, because it is standing in for herdr, not for us",
}

// namesFromPackage reads the ban list out of the name table — names.go and transport.go: every declared
// constant whose value is unambiguously a herdr name — dotted (socket methods, event types), a long flag, a
// `*Hook` (the underscore spelling of an event), or named like a herdr name (`Method*`, `Env*`) — plus the
// program name itself.
func namesFromPackage(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	banned := map[string]bool{Program: true}
	for _, path := range []string{"names.go", "transport.go"} {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("cannot read our own name table (%s): %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, expr := range value.Values {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					named := false
					for _, name := range value.Names {
						if strings.HasSuffix(name.Name, "Hook") {
							named = true
						}
						// The two kinds of name whose *value* cannot say it is herdr's: `MethodPing` is
						// "ping" and `EnvSocketPath` is "HERDR_SOCKET_PATH". No dot, no flag, nothing
						// English-looking about either — so the declaration is what bans them.
						if strings.HasPrefix(name.Name, "Method") || strings.HasPrefix(name.Name, "Env") {
							named = true
						}
					}
					if strings.Contains(text, ".") || strings.HasPrefix(text, "--") || named {
						banned[text] = true
					}
				}
			}
			return true
		})
	}

	// Sanity: a collector that silently finds nothing would make this whole file pass for the wrong reason.
	for _, must := range []string{
		MethodEventsSubscribe, MethodPing, FlagMachine, EventPaneCreated, EventPaneCreatedHook,
		EnvSessionFlag, EnvSocketPath,
	} {
		if !banned[must] {
			t.Fatalf("the ban list did not pick up %q out of names.go; this test is not checking what it says", must)
		}
	}

	// Path leaves that only exist inside the path builders in names.go, where they are assembled from
	// Program rather than written whole.
	for _, leaf := range []string{"herdr.sock", "plugins.json", "agent-detection"} {
		banned[leaf] = true
	}
	return banned
}

// commandWords are the herdr CLI words that are also ordinary English: the groups (subcommand namespaces)
// and the verbs. They are only a violation in a command-building shape.
func commandWords() map[string]bool {
	words := map[string]bool{}
	for _, w := range []string{
		GroupMachine, GroupAPI, GroupPlugin, GroupPane, GroupWorkspace, GroupAgent, GroupTerminal, GroupSession,
		VerbStatus, VerbList, VerbSnapshot, VerbExplain, VerbRename, VerbClose, VerbSendKeys, VerbSendText,
		VerbRun, VerbPrompt, VerbRead, VerbUnlink, VerbControl,
	} {
		words[w] = true
	}
	return words
}

// callNames are the calls that put a herdr command line together: our own client's two entry points.
//
// `exec.Command` and friends are deliberately absent. They are how this bridge runs `systemctl`,
// `launchctl`, `sqlite3` and its own executable, and a rule that reads their arguments reports
// `exec.Command("systemctl", "--user", "status", …)` as if it were `herdr status` — three false alarms
// the first time it ran. Nothing is lost: `exec`-ing herdr by hand would spell the program name
// (`herdr`), which the ban list above already catches.
var callNames = map[string]bool{
	"Run": true, "RunMachine": true,
}

func TestHerdrNamesLiveOnlyInThisPackage(t *testing.T) {
	banned := namesFromPackage(t)
	words := commandWords()

	root := filepath.Join("..", "..") // the bridge module
	self := filepath.Join("..", "..", "internal", "herdr")

	var violations []string
	files, skipped := 0, 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == self || path == filepath.Join(root, "build") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if reason, ok := allowed[rel]; ok {
			t.Logf("not checking %s: %s", rel, reason)
			skipped++
			return nil
		}
		files++
		violations = append(violations, inspect(t, path, rel, banned, words)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the bridge: %v", err)
	}
	if files == 0 {
		t.Fatal("no files were checked; the walk is looking in the wrong place")
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("%d herdr name(s) spelled outside internal/herdr — a herdr release that renames one of "+
			"these is a hunt through the whole bridge instead of one file. Ask this package for it "+
			"(names.go, commands.go) or add it there:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
	t.Logf("checked %d files (%d allowed): no herdr name outside this package", files, skipped)
}

// inspect reports the violations in one file, as "file:line: name".
func inspect(t *testing.T, path, rel string, banned, words map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", rel, err)
	}

	var found []string
	at := func(pos token.Pos, name, shape string) {
		found = append(found, rel+":"+strconv.Itoa(fset.Position(pos).Line)+": "+shape+name)
	}
	literal := func(expr ast.Expr) (string, bool) {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(lit.Value)
		return text, err == nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if text, ok := literal(node); ok && banned[text] {
				at(node.Pos(), text, "")
			}

		case *ast.CompositeLit:
			// []string{"pane", …}: a literal argv sentence.
			array, ok := node.Type.(*ast.ArrayType)
			if !ok || len(node.Elts) == 0 {
				return true
			}
			if ident, ok := array.Elt.(*ast.Ident); !ok || ident.Name != "string" {
				return true
			}
			// `[]string{Program, "pane", …}` spells the same sentence with the program name in front
			// instead of at the call site, so the element that matters is one further along.
			if ident, ok := node.Elts[0].(*ast.Ident); ok && ident.Name == "Program" && len(node.Elts) > 1 {
				if text, ok := literal(node.Elts[1]); ok && words[text] {
					at(node.Elts[1].Pos(), text, "literal command line after Program: ")
				}
				return true
			}
			if text, ok := literal(node.Elts[0]); ok && words[text] {
				at(node.Pos(), text, "literal command line: ")
			}

		case *ast.CallExpr:
			name := ""
			switch f := node.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			if !callNames[name] {
				return true
			}
			// The first few string literals, wherever they sit: `Run(ctx, host, "pane", "read")` puts the
			// group word second, and stopping at the first argument that is not a literal walks past it.
			// Bounded at three, because past that the arguments are ours again (a pane id, a label, a
			// piece of text), and a word like "list" is ordinary English in somebody else's command.
			literals := 0
			for _, arg := range node.Args {
				text, ok := literal(arg)
				if !ok {
					continue
				}
				if words[text] {
					at(arg.Pos(), text, "command word spelled as a "+name+" argument: ")
				}
				if literals++; literals == 3 {
					break
				}
			}
		}
		return true
	})
	return found
}
