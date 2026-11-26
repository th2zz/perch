package records

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The shapes in these fixtures are copied from the records on this machine, not invented: the
// point of this package is that the data was already written down, and a fixture written from
// imagination would test the imagination instead of the file.
//
// Both generations of codex's record are here, because both exist on this machine's disk and a
// live session may be either: the newer one keeps the current context in `usage` and the
// aggregates per turn and per thread, the older one puts everything in one `info` object — and
// the first version of the reader matched the *record's* type rather than the payload's, which
// is why it read nothing at all from either.

const codexSessionIDValue = "01a128ff-4c2c-7a20-b253-66798d3a28ae"

// A second, different id, for the tests where a pane's registration and its record must
// disagree: the disagreement is the thing under test.
const codexSessionIDOther = "01deadbe-ef00-4000-8000-000000000000"

func writeCodexRecord(t *testing.T, home string, lines ...string) {
	t.Helper()
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-14T07-16-17", lines...)
}

func writeCodexRecordNamed(t *testing.T, home, id, stamp string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "14")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "rollout-"+stamp+"-"+id+".jsonl")
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// holdRecordOpen starts a process tree whose *grandchild* holds the named file open, and returns
// the pid at the top of that tree.
//
// The shape is codex 0.163's: a foreground process that holds no record at all, wrapping the one
// that does. A reader that looks only at the pid it was given finds nothing — which is how the
// conversation face came back empty on a pane full of answers.
func holdRecordOpen(t *testing.T, path string) int {
	t.Helper()
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner.sh")
	mid := filepath.Join(dir, "mid.sh")
	outer := filepath.Join(dir, "outer.sh")
	if err := os.WriteFile(inner, []byte("exec 3>>\"$1\"\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatalf("write holder: %v", err)
	}
	if err := os.WriteFile(mid, []byte("sh \"$1\" \"$2\" &\nwait\n"), 0o755); err != nil {
		t.Fatalf("write middle: %v", err)
	}
	if err := os.WriteFile(outer, []byte("sh \"$1\" \"$2\" \"$3\" &\nwait\n"), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	cmd := exec.Command("sh", outer, mid, inner, path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	// Start is only "forked": wait until the grandchild has really opened the file, so the
	// reader under test is never racing the fixture.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if recordUnder(cmd.Process.Pid, filepath.Dir(path)) == path {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the holder tree never opened %s (children: %v)", path, processChildren(cmd.Process.Pid))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cmd.Process.Pid
}

func TestCodexNewerRecordShape(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		`{"type":"session_meta","payload":{"id":"`+codexSessionIDValue+`","cwd":"/home/x/workspace"}}`,
		`{"type":"turn_context","payload":{"model":"deepseek-flash","effort":"high",`+
			`"approval_policy":"never","sandbox_policy":{"type":"danger-full-access"},"cwd":"/home/x/workspace"}}`,
		`{"type":"token_usage_record","payload":{"usage":{"input_tokens":654200,"cached_input_tokens":640000,`+
			`"output_tokens":995,"total_tokens":655195},"thread_token_usage":{"input_tokens":1541443593,`+
			`"cached_input_tokens":1534388599,"output_tokens":7054994,"total_tokens":1541443593}}}`,
		// The window arrives in its own record, and inside the payload: this is the shape that
		// the first reader missed entirely.
		`{"type":"event_msg","payload":{"type":"token_count","model_context_window":996147}}`,
	)

	facts, ok := Read("codex", home, Where{Argv: []string{"node", "/x/codex", "resume", codexSessionIDValue}})
	if !ok {
		t.Fatal("no facts read from a record that is right there")
	}
	if facts.Model != "deepseek-flash" || facts.Effort != "high" {
		t.Errorf("model/effort = %q/%q", facts.Model, facts.Effort)
	}
	if facts.ContextUsed != 654200 {
		t.Errorf("context used = %d, want the input the last turn sent (654200)", facts.ContextUsed)
	}
	if facts.ContextWindow != 996147 {
		t.Errorf("window = %d, want 996147", facts.ContextWindow)
	}
	if facts.TotalTokens != 1541443593 || facts.CachedTokens != 1534388599 {
		t.Errorf("spend = %d total / %d cached", facts.TotalTokens, facts.CachedTokens)
	}
	if facts.Access != "full access, no approvals asked" {
		t.Errorf("access = %q", facts.Access)
	}
	if facts.SessionID != codexSessionIDValue {
		t.Errorf("session id = %q", facts.SessionID)
	}
}

func TestCodexFactsFindTheRecordByTheSessionHerdrReported(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		`{"type":"turn_context","payload":{"model":"gpt-5","effort":"high",`+
			`"approval_policy":"never","sandbox_policy":{"type":"danger-full-access"}}}`,
		`{"type":"token_usage_record","payload":{"usage":{"input_tokens":654200,"cached_input_tokens":640000,`+
			`"output_tokens":995,"total_tokens":655195},"thread_token_usage":{"input_tokens":1541443593,`+
			`"cached_input_tokens":1534388599,"output_tokens":7054994,"total_tokens":1541443593}}}`,
	)

	where := Where{
		PaneID: "wA:p1",
		Argv:   []string{"node", "/home/user/.local/opt/node/bin/codex"},
		// A real pid whose tree holds no record — the ordinary case while an agent runs a
		// tool: the registration must still be taken, not refused for lack of corroboration.
		PID:       os.Getpid(),
		SessionID: codexSessionIDValue,
	}
	facts, ok := Read("codex", home, where)
	if !ok {
		t.Fatal("herdr named the session; the reader still reported no facts")
	}
	if facts.Model != "gpt-5" {
		t.Errorf("model = %q", facts.Model)
	}
	if facts.TotalTokens != 1541443593 || facts.CachedTokens != 1534388599 {
		t.Errorf("spend = %d total / %d cached", facts.TotalTokens, facts.CachedTokens)
	}
	if facts.SessionID != codexSessionIDValue {
		t.Errorf("session id = %q, want %q", facts.SessionID, codexSessionIDValue)
	}
}

func TestCodexSessionIDComesOffTheRecordNameWhenNothingElseNamesIt(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	writeCodexRecord(t, home, `{"type":"turn_context","payload":{"model":"gpt-5","effort":"low"}}`)

	path := filepath.Join(home, ".codex", "sessions", "2026", "10", "14",
		"rollout-2026-10-14T07-16-17-"+codexSessionIDValue+".jsonl")
	held, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer held.Close()

	facts, ok := Read("codex", home, Where{
		PaneID: "wA:p1",
		Argv:   []string{"node", "/home/user/.local/opt/node/bin/codex"},
		PID:    os.Getpid(),
	})
	if !ok {
		t.Fatal("this process holds the record open; the reader reported no facts")
	}
	if facts.SessionID != codexSessionIDValue {
		t.Errorf("session id = %q, want the one in the file name %q", facts.SessionID, codexSessionIDValue)
	}
}

func TestCodexRolloutFindsTheRecordAGrandchildHoldsOpen(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	writeCodexRecord(t, home, `{"type":"turn_context","payload":{"model":"gpt-5","effort":"low"}}`)
	pid := holdRecordOpen(t, findCodexRollout(home, codexSessionIDValue))

	facts, _ := Read("codex", home, Where{
		PaneID: "wA:p1",
		Argv:   []string{"node", "/home/user/.local/opt/node/bin/codex"},
		PID:    pid,
	})
	if facts.SessionID != codexSessionIDValue || facts.Model != "gpt-5" {
		t.Errorf("session/model = %q/%q, want the record the tree holds", facts.SessionID, facts.Model)
	}
}

func TestCodexRolloutRefusesTheRegistrationWhenTheKernelShowsAnotherRecord(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	// A previous session's record, still on disk and still matching its id.
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-16T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	// The session the pane actually runs now, held open like codex holds it.
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-16T12-59-29",
		`{"type":"turn_context","payload":{"model":"gpt-5","effort":"high"}}`)
	pid := holdRecordOpen(t, findCodexRollout(home, codexSessionIDValue))

	facts, _ := Read("codex", home, Where{
		PaneID:    "wA:p1",
		Argv:      []string{"node", "/home/user/.local/opt/node/bin/codex"},
		PID:       pid,
		SessionID: codexSessionIDOther,
	})
	if facts.Model != "gpt-5" || facts.SessionID != codexSessionIDValue {
		t.Errorf("model/session = %q/%q; the registration named a record the pane does not hold",
			facts.Model, facts.SessionID)
	}
}

func TestCodexRolloutSeesThroughASymlinkedHome(t *testing.T) {
	real := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(real); err == nil {
		real = resolved
	}
	link := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	writeCodexRecordNamed(t, real, codexSessionIDOther, "2026-10-16T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	writeCodexRecordNamed(t, real, codexSessionIDValue, "2026-10-16T12-59-29",
		`{"type":"turn_context","payload":{"model":"gpt-5","effort":"high"}}`)
	pid := holdRecordOpen(t, findCodexRollout(real, codexSessionIDValue))

	// The home handed to the reader goes through the symlink, like a NixOS /home does; the
	// kernel's answer will carry the canonical path. The two spellings must still compare equal
	// — otherwise the stale registration would be served on exactly the machines that do this.
	facts, _ := Read("codex", link, Where{
		PaneID:    "wA:p1",
		Argv:      []string{"node", "/home/user/.local/opt/node/bin/codex"},
		PID:       pid,
		SessionID: codexSessionIDOther,
	})
	if facts.Model != "gpt-5" || facts.SessionID != codexSessionIDValue {
		t.Errorf("model/session = %q/%q; a symlinked home hid the record the pane holds",
			facts.Model, facts.SessionID)
	}
}

func TestCodexRolloutPrefersTheCommandLineOverAStaleRegistration(t *testing.T) {
	home := t.TempDir()
	// The stale registration's record is still on disk — the ordinary residue of a resumed
	// pane — so nothing but the freshness of the command line can tell the two apart.
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-16T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-16T12-59-29",
		`{"type":"turn_context","payload":{"model":"gpt-5","effort":"high"}}`)

	facts, _ := Read("codex", home, Where{
		PaneID:    "wA:p1",
		Argv:      []string{"node", "/home/user/.local/opt/node/bin/codex", "resume", codexSessionIDValue},
		PID:       os.Getpid(),
		SessionID: codexSessionIDOther,
	})
	if facts.SessionID != codexSessionIDValue || facts.Model != "gpt-5" {
		t.Errorf("session/model = %q/%q; the stale registration outranked the command line",
			facts.SessionID, facts.Model)
	}
}

func TestCodexRolloutDoesNotMatchAPartialSessionValue(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, `{"type":"turn_context","payload":{"model":"gpt-5","effort":"low"}}`)

	// "rollout" is a fragment that would glob-match the record above. A reader that globs a
	// fragment serves some other session's conversation under this pane's name; the honest
	// answer about a value that names no session is "nothing readable".
	for _, fragment := range []string{"rollout", codexSessionIDValue[:8], codexSessionIDValue[:12]} {
		facts, _ := Read("codex", home, Where{
			PaneID:    "wA:p1",
			Argv:      []string{"node", "/home/user/.local/opt/node/bin/codex"},
			PID:       os.Getpid(),
			SessionID: fragment,
		})
		if facts.SessionID != "" || facts.Model != "" {
			t.Errorf("a fragment %q was read as a session: model/session = %q/%q",
				fragment, facts.Model, facts.SessionID)
		}
	}
}

// The state database is an inference about a launch, and an inference must never fill a name's
// place: when the registration or the command line says which record this pane runs and that
// record is not on disk, the honest answer is nothing at all — a matching row in the state
// database would be some *other* session wearing this pane's name.
func TestCodexRolloutRefusesWhenANamedRecordIsMissing(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	// The row points at a record that is really there; the pane's *name* — the record it says
	// it runs — was never written to disk. Only the name's absence can decide this.
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: now + 900, originator: "codex-tui"},
	)

	cases := []struct {
		name  string
		where Where
	}{
		{"the registration names the missing record", Where{
			PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
			SessionID: codexSessionIDValue,
		}},
		{"the command line names the missing record", Where{
			PaneID: "w1:p1", CWD: cwd, PID: pid,
			Argv: []string{"node", "/x/bin/codex", "resume", codexSessionIDValue},
		}},
	}
	for _, tc := range cases {
		facts, _ := Read("codex", home, tc.where)
		if facts.SessionID != "" || facts.Model != "" {
			t.Errorf("%s: served %q/%q by inference where a name found no record",
				tc.name, facts.SessionID, facts.Model)
		}
	}
}

// The state database is shared with a live codex, and every question about it sits on the
// phone's polling path — so a tool that hangs, or one that exits while a grandchild keeps its
// output pipe open, must both end in the time the caller can afford. The bound asserted here is
// generous; what it separates is "bounded" from "waits for the tool".
// The databases the bridge reads belong to a live codex that is writing them. The one thing the
// reader must never be is a second writer: sqlite3 without -readonly opens the file read-write,
// and a reader that cannot touch the file is also a reader that cannot collide with the owner.
func TestQuerySQLiteAsksReadOnly(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	tool := filepath.Join(dir, "sqlite3")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n", argsFile)
	if err := os.WriteFile(tool, []byte(body), 0o755); err != nil {
		t.Fatalf("write the fake tool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := querySQLite(ctx, tool, "the.db", "select 1"); err != nil {
		t.Fatalf("query: %v", err)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the tool recorded nothing: %v", err)
	}
	if want := "-readonly\nthe.db\nselect 1\n"; string(got) != want {
		t.Errorf("the tool was called with %q, want %q", got, want)
	}
}

func TestQuerySQLiteIsBounded(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		// Never answers: only the deadline can end it.
		{"a tool that hangs", "#!/bin/sh\nexec sleep 60\n"},
		// Answers, but leaves the output pipe in a grandchild's hands: only WaitDelay can end it.
		{"a grandchild holding the pipe", "#!/bin/sh\nsleep 5 &\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := filepath.Join(t.TempDir(), "sqlite3")
			if err := os.WriteFile(tool, []byte(tc.body), 0o755); err != nil {
				t.Fatalf("write the fake tool: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := querySQLite(ctx, tool, "db", "select 1")
			elapsed := time.Since(start)
			if err == nil {
				t.Errorf("a tool that never answered was reported as an answer")
			}
			if elapsed > 3*time.Second {
				t.Errorf("waited %v; the caller's budget does not bound the tool", elapsed)
			}
		})
	}
}

func TestCodexOlderRecordShape(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		`{"type":"turn_context","payload":{"model":"gpt-5","effort":"low"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":`+
			`{"input_tokens":30652,"cached_input_tokens":30080,"output_tokens":280,"total_tokens":30932},`+
			`"last_token_usage":{"input_tokens":10382,"cached_input_tokens":10240,"output_tokens":16},`+
			`"model_context_window":996147}}}`,
	)

	facts, _ := Read("codex", home, Where{Argv: []string{"codex", "resume", codexSessionIDValue}})
	if facts.ContextUsed != 10382 {
		t.Errorf("context used = %d, want the last turn's input", facts.ContextUsed)
	}
	if facts.TotalTokens != 30932 || facts.CachedTokens != 30080 {
		t.Errorf("spend = %d total / %d cached", facts.TotalTokens, facts.CachedTokens)
	}
	if facts.ContextWindow != 996147 {
		t.Errorf("window = %d", facts.ContextWindow)
	}
}

func TestUnreadableAndNothingRecordedAreDifferentAnswers(t *testing.T) {
	home := t.TempDir()

	readable, ok := Read("codex", home, Where{})
	if !ok {
		t.Error("codex is a harness this package knows how to read, even when it finds nothing")
	}
	if !readable.Readable || !readable.isEmpty() {
		t.Errorf("codex with no records: readable=%v facts=%+v", readable.Readable, readable)
	}

	// A pid whose /proc entry does not exist is the same kind of answer: readable, empty.
	claude, ok := Read("claude", home, Where{PID: 999999})
	if !ok || !claude.Readable {
		t.Errorf("claude is readable even when the process is gone: %+v ok=%v", claude, ok)
	}

	// A harness nobody has written an adapter for says *that*, and does not go looking at
	// somebody else's files.
	unknown, ok := Read("gemini", home, Where{})
	if ok || unknown.Readable {
		t.Errorf("an unmeasured harness must report itself as unreadable, got %+v ok=%v", unknown, ok)
	}
	if !unknown.isEmpty() {
		t.Errorf("an unreadable harness must carry no facts at all: %+v", unknown)
	}
}

func TestSessionIDComesOffTheCommandLine(t *testing.T) {
	got := sessionIDFromArgv([]string{"node", "/home/x/.nvm/bin/codex", "resume", codexSessionIDValue})
	if got != codexSessionIDValue {
		t.Errorf("session id = %q", got)
	}
	if id := sessionIDFromArgv([]string{"codex", "--model", "gpt-5"}); id != "" {
		t.Errorf("a fresh session has no id on its command line, got %q", id)
	}
}

// A record's file name carries the hour the session started as well as the id, so the id is
// matched at the end of the name and nowhere else. Anchoring the search the way the command line
// is anchored is how the id came back empty from a file that is named after it.
func TestSessionIDComesOffARecordName(t *testing.T) {
	name := "rollout-2026-10-16T12-59-29-" + codexSessionIDValue + ".jsonl"
	if got := sessionIDFromRecordName("/home/x/.codex/sessions/2026/10/16/" + name); got != codexSessionIDValue {
		t.Errorf("session id = %q, want %q", got, codexSessionIDValue)
	}
	if got := sessionIDFromRecordName("/home/x/.codex/sessions/2026/10/16/rollout-2026-10-16T12-59-29.jsonl"); got != "" {
		t.Errorf("a record named after no session reported one: %q", got)
	}
}

func TestTheWordsForAPermissionMode(t *testing.T) {
	cases := map[string]struct{ approval, sandbox, want string }{
		"their own machine, no prompts":  {"never", "danger-full-access", "full access, no approvals asked"},
		"approved on request, workspace": {"on-request", "workspace-write", "can write in the workspace, asks before acting"},
		"sandboxed to reading":           {"never", "read-only", "read-only, no approvals asked"},
		"nothing known about the policy": {"", "", ""},
		"only one half known":            {"never", "", "no approvals asked"},
	}
	for name, tc := range cases {
		if got := accessWords(tc.approval, tc.sandbox); got != tc.want {
			t.Errorf("%s: accessWords = %q, want %q", name, got, tc.want)
		}
	}
}

func TestClaudeTranscript(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-home-x-workspace")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "828d8ea3-a19e-4110-8793-441911745602.jsonl")
	body := `{"type":"assistant","message":{"model":"deepseek-flash","usage":{"input_tokens":151,` +
		`"cache_read_input_tokens":15488,"cache_creation_input_tokens":0,"output_tokens":10}}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// In production the file is found through the pid that holds it open; the parsing is the
	// part worth testing here, and it is told which file to read.
	facts, ok := claudeFactsFrom(path)
	if !ok {
		t.Fatal("no facts from a transcript that is right there")
	}
	if facts.Model != "deepseek-flash" {
		t.Errorf("model = %q", facts.Model)
	}
	// The context is what the last request carried: input plus both kinds of cache. That is the
	// number claude's own status line turns into a percentage, and the transcript names no
	// window — so the panel shows tokens and no percentage, rather than inventing one.
	if want := int64(151 + 15488); facts.ContextUsed != want {
		t.Errorf("context used = %d, want %d", facts.ContextUsed, want)
	}
	if facts.CachedTokens != 15488 {
		t.Errorf("cached = %d", facts.CachedTokens)
	}
	if facts.SessionID != "828d8ea3-a19e-4110-8793-441911745602" {
		t.Errorf("session id = %q", facts.SessionID)
	}
}
