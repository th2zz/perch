package records

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The shapes in these fixtures are the ones measured on this machine (2026-10-17), not invented:
// a codex pane's foreground process is a wrapper that names the codex script as its first
// argument (node on PATH installs), the real binary under it is named codex, and a fresh
// session's `threads` row appears ~900 ms after the codex process starts — long before the first
// prompt is typed, which is what lets a launch be matched to its row.
//
// The two shapes are exercised by their own fixtures, not one fixture doing both jobs: a single
// fake that matched through either rule would keep passing if one rule was deleted.

// fakeCodexProcess starts a process whose shape is the wrapper's: an ordinary binary (bash) and
// a codex script as its first argument, with the script's child underneath. It returns the pid
// of the process at the top of the tree — the same position herdr reports as the pane's
// foreground pid.
func fakeCodexProcess(t *testing.T, cwd string) int {
	t.Helper()
	script := filepath.Join(t.TempDir(), "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fake pane tree: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	for len(codexStartWindows(cmd.Process.Pid)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the fake codex under pid %d never started", cmd.Process.Pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cmd.Process.Pid
}

// fakeCodexBinary starts a process shaped like the binary itself: the executable is named codex,
// while its argv deliberately does not name codex anywhere, so only the binary's own name can
// identify it.
func fakeCodexBinary(t *testing.T, cwd string) int {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on this machine")
	}
	body, err := os.ReadFile(sleep)
	if err != nil {
		t.Skip("cannot read the sleep binary")
	}
	bin := filepath.Join(t.TempDir(), "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(bin, body, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	cmd := exec.Command(bin, "60")
	cmd.Args = []string{"wyrm", "60"}
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the binary: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

// fakeNonCodexProcess starts an ordinary command whose own line merely *mentions* a codex path,
// the way `git diff /bin/codex` or `npm test -- /bin/codex` would: nothing about it is a launch.
func fakeNonCodexProcess(t *testing.T, cwd string) int {
	t.Helper()
	// Two commands, so bash cannot replace itself with the sleep and keeps its own command line.
	cmd := exec.Command("bash", "-c", "sleep 60; true", "/somewhere/bin/codex")
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the untrusted-looking process: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

// stateRow is one `threads` row of codex's state database, spelled with the columns this
// package reads.
type stateRow struct {
	id         string
	path       string
	cwd        string
	createdMS  int64
	updatedMS  int64
	originator string
	name       string
	title      string
}

// writeStateDB writes the part of codex's own state database that names sessions. It is written
// through the sqlite3 tool because that is what the bridge itself reads it with — a test that
// built the file differently would test a machine nobody has.
func writeStateDB(t *testing.T, home string, rows ...stateRow) {
	t.Helper()
	tool := sqliteTool()
	if tool == "" {
		t.Skip("no sqlite3 on this machine; the state database is read through it")
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stmts := []string{
		`create table threads (id text primary key, rollout_path text not null, ` +
			`created_at_ms integer not null, updated_at_ms integer, cwd text not null, originator text not null, ` +
			`name text, title text)`,
	}
	for _, r := range rows {
		updated := r.updatedMS
		if updated == 0 {
			updated = r.createdMS
		}
		stmts = append(stmts, fmt.Sprintf(
			`insert into threads (id, rollout_path, created_at_ms, updated_at_ms, cwd, originator, name, title) `+
				`values ('%s','%s',%d,%d,'%s','%s','%s','%s')`,
			r.id, r.path, r.createdMS, updated, r.cwd, r.originator, r.name, r.title))
	}
	db := filepath.Join(home, ".codex", "state_5.sqlite")
	if out, err := exec.Command(tool, db, strings.Join(stmts, ";\n")).CombinedOutput(); err != nil {
		t.Fatalf("build state database: %v: %s", err, out)
	}
}

// A fresh codex pane on this machine registers its session with nobody: the SessionStart hook
// runs inside the shared app-server daemon, whose environment still points at the pane that
// first started it, and the daemon — not the pane — holds the rollout open. The state database
// row is what codex still wrote down for this launch, and reading the pane through it is the
// whole point of this source. Seen red before this source existed.
func TestCodexRolloutReadsTheStateDatabaseForAPaneThatRegisteredNowhere(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	if record == "" {
		t.Fatal("fixture record not written")
	}

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: now + 900, originator: "codex-tui"},
	)

	where := Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	}
	facts, ok := Read("codex", home, where)
	if !ok {
		t.Fatal("codex stays readable even when the pane named nobody")
	}
	if facts.SessionID != codexSessionIDValue || facts.Model != "gpt-5-state" {
		t.Errorf("session/model = %q/%q, want the row born with this launch",
			facts.SessionID, facts.Model)
	}
}

// The hook of a fresh codex pane can report its session to a *different* pane (the daemon keeps
// the first pane's environment), so herdr's registration names a session this pane is not
// running. If that registration is served, two panes show the same conversation — measured on
// this machine on 2026-10-17. The state database says which session this launch really is, so
// the stale registration is replaced by the pane's own session.
func TestCodexRolloutUsesItsOwnSessionWhenTheRegistrationIsStale(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}

	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"not-this-pane","effort":"high"}}`)
	stale := findCodexRollout(home, codexSessionIDOther)
	if stale == "" {
		t.Fatal("stale registration fixture not written")
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-05-00",
		`{"type":"turn_context","payload":{"model":"this-pane","effort":"high"}}`)
	own := findCodexRollout(home, codexSessionIDValue)
	if own == "" {
		t.Fatal("own record fixture not written")
	}

	pid := fakeCodexProcess(t, cwd)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: own, cwd: cwd,
			createdMS: time.Now().UnixMilli() + 900, originator: "codex-tui"},
	)

	got, id := codexRollout(home, Where{
		PaneID:    "wStale:p1",
		CWD:       cwd,
		Argv:      []string{"node", "/x/bin/codex"},
		PID:       pid,
		SessionID: codexSessionIDOther,
	})
	if !sameFile(got, own) || id != codexSessionIDValue {
		t.Fatalf("served %q (%s); want this pane's own record %q (%s)",
			got, id, own, codexSessionIDValue)
	}
}

func TestCodexRolloutReadsAStateRowTheAppServerCreatedBeforeTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T12-46-05",
		`{"type":"turn_context","payload":{"model":"gpt-5-app-server","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	if record == "" {
		t.Fatal("fixture record not written")
	}

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	touchRecord(t, record)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 5_000, updatedMS: launch + 1_000, originator: "codex-tui"},
	)

	facts, ok := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if !ok {
		t.Fatal("codex went unreadable because its row predates the TUI process")
	}
	if facts.SessionID != codexSessionIDValue || facts.Model != "gpt-5-app-server" {
		t.Errorf("session/model = %q/%q, want the app-server's row for this launch",
			facts.SessionID, facts.Model)
	}
}

func TestConversationReadsAStateRowTheAppServerCreatedBeforeTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T12-46-05",
		lineCodexSessionMeta, lineCodexUserMessage, lineCodexFinalAnswer)
	record := findCodexRollout(home, codexSessionIDValue)
	if record == "" {
		t.Fatal("fixture record not written")
	}

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record,
			cwd: cwd, createdMS: launch - 5_000, updatedMS: launch + 1_000, originator: "codex-tui"},
	)

	transcript, readable := Conversation("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if !readable {
		t.Fatal("conversation went unreadable because its row predates the TUI process")
	}
	if len(transcript.Messages) != 2 {
		t.Fatalf("messages = %+v, want the user message and the final answer", transcript.Messages)
	}
}

func TestCodexRolloutRefusesAnOldStateRowThatWasNotWrittenAfterTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"not-this-pane","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 5_000, updatedMS: launch - 4_000, originator: "codex-tui"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if facts.SessionID != "" || facts.Model != "" {
		t.Errorf("served %q/%q from a row that stopped being written before this launch",
			facts.SessionID, facts.Model)
	}
}

func TestCodexRolloutIgnoresADeadRowBetweenTheAppServerRowAndTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T12-46-05",
		`{"type":"turn_context","payload":{"model":"gpt-5-app-server","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 5_000, updatedMS: launch + 1_000, originator: "codex-tui"},
		stateRow{id: codexSessionIDOther, path: "/nowhere/dead.jsonl", cwd: cwd,
			createdMS: launch - 3_000, updatedMS: launch - 4_000, originator: "codex-tui"},
	)

	got, id := codexRollout(home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if !sameFile(got, record) || id != codexSessionIDValue {
		t.Fatalf("served %q (%s); want %q (%s)", got, id, record, codexSessionIDValue)
	}
}

func TestCodexRolloutRefusesAnActiveRowBetweenTheAppServerRowAndTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T12-46-05",
		`{"type":"turn_context","payload":{"model":"gpt-5-app-server","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 5_000, updatedMS: launch + 1_000, originator: "codex-tui"},
		stateRow{id: codexSessionIDOther, path: "/nowhere/active.jsonl", cwd: cwd,
			createdMS: launch - 3_000, updatedMS: launch + 1_000, originator: "codex-tui"},
	)

	got, id := codexRollout(home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if got != "" || id != "" {
		t.Fatalf("served %q (%s) while a newer active row was unexplained", got, id)
	}
}

// codex's /new keeps the process running and starts a session whose row is anchored to the
// /new moment — no launch explains it. The pane may be living in that session now, so the
// matched row is a conversation the pane has moved on from, and serving it would be the one
// wrong answer this source must never give. Refuse.
func TestCodexRolloutRefusesWhenASessionStartedAfterTheMatchedOne(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: now + 900, originator: "codex-tui"},
		// A minute after the launch, with the process still running: the row of a /new, not of
		// any launch.
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: now + 60_000, originator: "codex-tui"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if facts.SessionID != "" || facts.Model != "" {
		t.Errorf("served %q/%q while a newer session in the same directory belonged to nobody",
			facts.SessionID, facts.Model)
	}
}

// A second pane in the same directory is a launch, and the row born with it is explained by
// that launch: the older pane's session is still its own and stays readable.
func TestCodexRolloutServesWhileAYoungerLaunchExplainsItsRow(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	// The younger pane has to have started clearly after this one, or the two launches are the
	// same launch as far as the moment can tell and nothing is provable either way.
	time.Sleep(1500 * time.Millisecond)
	younger := fakeCodexProcess(t, cwd)
	theirRow := processStartMS(younger) + 900

	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: now + 900, originator: "codex-tui"},
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: theirRow, originator: "codex-tui"},
	)

	facts, ok := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if !ok {
		t.Fatal("codex went unreadable because another pane's launch was counted against it")
	}
	if facts.SessionID != codexSessionIDValue {
		t.Errorf("session = %q, want the row born with this pane's launch", facts.SessionID)
	}
}

// A row older than the launch cannot be this pane's session, and reading it anyway would serve
// somebody else's conversation. Nothing may be read at all.
func TestCodexRolloutRefusesAThreadThatPredatesTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: now - 3_600_000, originator: "codex-tui"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if facts.SessionID != "" || facts.Model != "" {
		t.Errorf("read %q/%q from a row that predates the launch", facts.SessionID, facts.Model)
	}
}

// A codex process can restart on an existing session: the row is hours old, but the managed
// app-server keeps updating it after the new launch. That is a resume, not the old row the
// launch-window rule exists to refuse.
func TestCodexRolloutReadsAResumedStateRowUpdatedAfterTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-resumed","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	touchRecordAfter(t, record, launch+1000)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 55*3600*1000, updatedMS: launch + 1000,
			originator: "codex-tui", name: "Checkout perch readable=true"},
	)

	facts, ok := Read("codex", home, Where{
		PaneID: "wResumed:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
		Title: "Checkout perch readable=true | workspace",
	})
	if !ok || facts.SessionID != codexSessionIDValue || facts.Model != "gpt-5-resumed" {
		t.Fatalf("resumed session = %q/%q (ok=%v), want its own row", facts.SessionID, facts.Model, ok)
	}
}

func touchRecord(t *testing.T, path string) {
	t.Helper()
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatalf("touch record: %v", err)
	}
}

func touchRecordAfter(t *testing.T, path string, ms int64) {
	t.Helper()
	when := time.UnixMilli(ms)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("touch record: %v", err)
	}
}

func TestCodexRolloutUsesThePersistedSessionWhenFreshSourcesFail(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"persisted","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	pid := fakeCodexProcess(t, cwd)

	got, id := codexRollout(home, Where{
		PaneID: "wPersisted:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
		PreferredSessionID: codexSessionIDValue,
	})
	if !sameFile(got, record) || id != codexSessionIDValue {
		t.Fatalf("persisted fallback served %q (%s), want %q", got, id, record)
	}
}

func TestCodexRolloutRefusesTwoResumedRowsUpdatedAfterTheLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"one","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"two","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 55*3600*1000, updatedMS: now, originator: "codex-tui",
			name: "one"},
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: launch - 54*3600*1000, updatedMS: now, originator: "codex-tui",
			name: "two"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "wTwoResumed:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
		Title: "one | workspace",
	})
	if facts.SessionID != "" {
		t.Errorf("two active resumed rows were resolved by a coin toss: %q", facts.SessionID)
	}
}

func TestCodexRolloutRefusesAResumedRowWithAnotherPanesTitle(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"somebody-else","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 55*3600*1000, updatedMS: time.Now().UnixMilli(),
			originator: "codex-tui", name: "Clean up old Codex sessions"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "wWrongTitle:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
		Title: "Checkout perch readable=true | workspace",
	})
	if facts.SessionID != "" {
		t.Errorf("served another pane's resumed row: %q", facts.SessionID)
	}
}

func TestCodexRolloutRefusesAResumedRowWhenANewSessionExists(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"old","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"new","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch - 55*3600*1000, updatedMS: time.Now().UnixMilli(),
			originator: "codex-tui", name: "old"},
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: launch + 200_000, updatedMS: launch + 200_000,
			originator: "codex-tui", name: "new"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "wNewSession:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
		Title: "old | workspace",
	})
	if facts.SessionID != "" {
		t.Errorf("served a resumed row while a new session existed: %q", facts.SessionID)
	}
}

// Two rows born at the same moment are two panes launched almost together, and the same
// directory is where both were started: whichever is served would be a coin toss, and a coin
// toss is how a phone shows the wrong conversation. Refusing is the honest answer.
func TestCodexRolloutRefusesTwoThreadsBornWithTheSameLaunch(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	now := time.Now().UnixMilli()
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: now + 900, originator: "codex-tui"},
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: now + 910, originator: "codex-tui"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if facts.SessionID != "" || facts.Model != "" {
		t.Errorf("a near-tie was resolved by a coin toss: %q/%q", facts.SessionID, facts.Model)
	}
}

// The window around a launch: the row must be born after the process starts (with a floor, so
// an earlier launch's row cannot pose as this one's) and inside the ceiling (a cold app-server
// spawn still counts). The winner must also clear the lead, or the two launches were the same
// launch as far as this source can tell.
// A pane in a directory with a long history: rows older than the matched one pile dozens deep
// (every earlier session ever run there). A page full of *older* rows is not a page of
// successors — the guard reads what is newer, and a busy directory must not go unreadable with
// age. Seen red while the successor question scanned the whole lookbehind and refused at a full
// page of history.
func TestCodexRolloutKeepsServingThroughOlderHistory(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	record := findCodexRollout(home, codexSessionIDValue)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	rows := make([]stateRow, 0, 71)
	// Seventy older sessions, one second apart, all inside the two-minute lookbehind the
	// successor question once scanned whole: enough to fill the page it refused at.
	for i := 0; i < 70; i++ {
		rows = append(rows, stateRow{
			id:         fmt.Sprintf("01history-0000-7000-8000-%012d", i),
			path:       fmt.Sprintf("/nowhere/rollout-history-%02d.jsonl", i),
			cwd:        cwd,
			createdMS:  launch - 170_000 + int64(i)*1_000,
			originator: "codex-tui",
		})
	}
	rows = append(rows, stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
		createdMS: launch + 900, originator: "codex-tui"})
	writeStateDB(t, home, rows...)

	facts, ok := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if !ok {
		t.Fatal("a pane whose directory merely has history went unreadable")
	}
	if facts.SessionID != codexSessionIDValue {
		t.Errorf("session = %q, want the row born with this launch", facts.SessionID)
	}
}

// A newer row born long past every ceiling — a session that began minutes after this launch,
// with no pane behind it running anymore — is exactly what the uncovered question exists for:
// no window question can see it, and serving the older match would show a conversation the
// directory has moved on from.
func TestCodexRolloutRefusesAThreadPastEveryWindow(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	writeCodexRecordNamed(t, home, codexSessionIDValue, "2026-10-17T09-00-00",
		`{"type":"turn_context","payload":{"model":"gpt-5-state","effort":"high"}}`)
	writeCodexRecordNamed(t, home, codexSessionIDOther, "2026-10-17T09-00-01",
		`{"type":"turn_context","payload":{"model":"gpt-4o","effort":"low"}}`)
	record := findCodexRollout(home, codexSessionIDValue)
	other := findCodexRollout(home, codexSessionIDOther)

	pid := fakeCodexProcess(t, cwd)
	launch := processStartMS(pid)
	writeStateDB(t, home,
		stateRow{id: codexSessionIDValue, path: record, cwd: cwd,
			createdMS: launch + 900, originator: "codex-tui"},
		// Past the ceiling, so this pane's own window cannot hold it; nobody else is running.
		stateRow{id: codexSessionIDOther, path: other, cwd: cwd,
			createdMS: launch + 200_000, originator: "codex-tui"},
	)

	facts, _ := Read("codex", home, Where{
		PaneID: "w1:p1", CWD: cwd, Argv: []string{"node", "/x/bin/codex"}, PID: pid,
	})
	if facts.SessionID != "" || facts.Model != "" {
		t.Errorf("served %q/%q while a newer row belonged to nobody", facts.SessionID, facts.Model)
	}
}

func TestPickStateThread(t *testing.T) {
	window := []codexWindow{{startMS: 1_000_000, cwd: "/w"}}
	cases := []struct {
		name string
		rows []stateThread
		want string
	}{
		{"born just after the launch", []stateThread{{id: "a", createdMS: 1_000_900}}, "a"},
		{"straight at the floor", []stateThread{{id: "a", createdMS: 1_000_300}}, "a"},
		{"below the floor, an earlier launch's row", []stateThread{{id: "a", createdMS: 1_000_100}}, ""},
		{"before the launch but written after it",
			[]stateThread{{id: "a", createdMS: 1_000_000 - 5_000, updatedMS: 1_000_000 + 1_000}}, "a"},
		{"before the launch and already stale",
			[]stateThread{{id: "a", createdMS: 1_000_000 - 5_000, updatedMS: 1_000_000 - 4_000}}, ""},
		{"older than the pre-launch window",
			[]stateThread{{id: "a", createdMS: 1_000_000 - 31_000, updatedMS: 1_000_000 + 1_000}}, ""},
		{"at the ceiling", []stateThread{{id: "a", createdMS: 1_180_000}}, "a"},
		{"past the ceiling", []stateThread{{id: "a", createdMS: 1_180_001}}, ""},
		{"an hour old", []stateThread{{id: "a", createdMS: 1_000_000 - 3_600_000}}, ""},
		{"the earliest with a lead wins", []stateThread{
			{id: "b", createdMS: 1_003_000}, {id: "a", createdMS: 1_000_900}}, "a"},
		{"a near follower refuses", []stateThread{
			{id: "a", createdMS: 1_000_900}, {id: "b", createdMS: 1_001_100}}, ""},
		{"an exact tie refuses", []stateThread{
			{id: "a", createdMS: 1_000_900}, {id: "b", createdMS: 1_000_900}}, ""},
		{"no rows, no answer", nil, ""},
	}
	for _, tc := range cases {
		got, ok := pickStateThread(tc.rows, window)
		if tc.want == "" {
			if ok {
				t.Errorf("%s: picked %q; refusal expected", tc.name, got.id)
			}
			continue
		}
		if !ok || got.id != tc.want {
			t.Errorf("%s: picked %q (ok=%v), want %q", tc.name, got.id, ok, tc.want)
		}
	}

	// A second launch under the same pane (a shell inside a codex session, say) licenses rows
	// born with that later launch too.
	two := []codexWindow{{startMS: 1_000_000, cwd: "/w"}, {startMS: 2_000_000, cwd: "/w"}}
	if got, ok := pickStateThread([]stateThread{{id: "b", createdMS: 2_000_500}}, two); !ok || got.id != "b" {
		t.Errorf("a row born with the second launch was not licensed: %q ok=%v", got.id, ok)
	}

	// One launch can present two codex processes and so two windows: the wrapper names the
	// script, the script's child is the binary itself (both halves measured on this machine).
	// A row born with the binary sits nearer the later window than an earlier row sits to the
	// wrapper, and proximity alone would crown it. The decision is on creation time: the
	// earliest row wins, and it leads the next one by 1.5s here — clear enough to serve.
	pair := []codexWindow{{startMS: 1_000_000, cwd: "/w"}, {startMS: 1_002_000, cwd: "/w"}}
	if got, ok := pickStateThread([]stateThread{
		{id: "b", createdMS: 1_002_500}, {id: "a", createdMS: 1_001_000},
	}, pair); !ok || got.id != "a" {
		t.Errorf("a row nearer the later window outranked the earlier row: %q ok=%v", got.id, ok)
	}
}

// The successor rule as a table: it reads two things only — whether a newer row sits outside
// every live launch's window (nothing can have written it) or inside one but short of being
// that window's winner (its launch would have written the winner first). Both are successors.
func TestUnexplainedSuccessor(t *testing.T) {
	thread := stateThread{id: "m", createdMS: 10_000, cwd: "/w"}
	launch := codexWindow{startMS: 9_000, cwd: "/w"} // window 9_300–189_000
	later := codexWindow{startMS: 12_000, cwd: "/w"}
	cases := []struct {
		name       string
		launches   []codexWindow
		windowRows [][]stateThread
		uncovered  []stateThread
		want       bool
	}{
		{
			name:       "the matched row's own window has nothing new",
			launches:   []codexWindow{launch},
			windowRows: [][]stateThread{{{id: "m", createdMS: 10_000}}},
		},
		{
			name:     "a younger launch's winner is its own row",
			launches: []codexWindow{launch, later},
			windowRows: [][]stateThread{
				{{id: "m", createdMS: 10_000}},
				{{id: "n", createdMS: 12_900}},
			},
		},
		{
			name:     "a second row inside the same window is shadowed",
			launches: []codexWindow{launch, later},
			windowRows: [][]stateThread{
				{{id: "m", createdMS: 10_000}},
				{{id: "n", createdMS: 12_900}, {id: "s", createdMS: 13_100}},
			},
			want: true,
		},
		{
			name:       "a row outside every window",
			launches:   []codexWindow{launch},
			windowRows: [][]stateThread{{{id: "m", createdMS: 10_000}}},
			uncovered:  []stateThread{{id: "u", createdMS: 200_000}},
			want:       true,
		},
		{
			name:       "an older row is not a successor even when unexplained",
			launches:   []codexWindow{launch},
			windowRows: [][]stateThread{{{id: "old", createdMS: 9_500}, {id: "m", createdMS: 10_000}}},
		},
		{
			name: "nothing running at all",
		},
	}
	for _, tc := range cases {
		if got := unexplainedSuccessor(thread, tc.launches, tc.windowRows, tc.uncovered); got != tc.want {
			t.Errorf("%s: unexplainedSuccessor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseStateThreads(t *testing.T) {
	out := "id-a\t/home/x/rollout-a.jsonl\t1000\t/w\t1100\n" +
		"\n" + // a row whose created_at_ms is null: the whole concatenation prints as an empty line
		"id-b\t/p\tnot-a-number\t/w\t1200\n" +
		"id-d\t/p\t2000\n" + // a row that lost a column cannot be read
		"id-c\t/home/x/rollout-c.jsonl\t2000\t/w\t2100\n"
	got := parseStateThreads([]byte(out))
	if len(got) != 2 {
		t.Fatalf("parsed %d rows from %q", len(got), out)
	}
	if got[0].id != "id-a" || got[0].path != "/home/x/rollout-a.jsonl" ||
		got[0].createdMS != 1000 || got[0].updatedMS != 1100 || got[0].cwd != "/w" {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[1].id != "id-c" || got[1].createdMS != 2000 || got[1].cwd != "/w" {
		t.Errorf("row 1 = %+v", got[1])
	}
}

func TestNewestCodexStateDatabase(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"state_2.sqlite", "state_4.sqlite", "state_10.sqlite"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// The companion files of a live database must not be mistaken for one.
	if err := os.WriteFile(filepath.Join(dir, "state_10.sqlite-shm"), []byte{}, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := newestCodexStateDB(home); got != filepath.Join(dir, "state_10.sqlite") {
		t.Errorf("newest state database = %q", got)
	}
	if got := newestCodexStateDB(t.TempDir()); got != "" {
		t.Errorf("a home with no state database reported %q", got)
	}
}

// The wrapper's shape: an ordinary foreground binary whose first argument is the codex script.
// The launch is found through that argument, and a process that merely *mentions* a codex path
// further down its command line — `git diff /bin/codex` in a pane — is not a launch.
func TestCodexStartWindowsFindsTheWrappedCodexAndIgnoresLaterArguments(t *testing.T) {
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	before := time.Now().UnixMilli()
	pid := fakeCodexProcess(t, cwd)
	after := time.Now().UnixMilli()

	windows := codexStartWindows(pid)
	if len(windows) != 1 {
		t.Fatalf("launches found = %d (%+v), want the one under pid %d", len(windows), windows, pid)
	}
	if windows[0].cwd != cwd {
		t.Errorf("launch directory = %q, want %q", windows[0].cwd, cwd)
	}
	// The launch cannot precede the fixture, and a clock that puts it minutes away means the
	// epoch arithmetic is wrong.
	if windows[0].startMS < before-2_000 || windows[0].startMS > after+2_000 {
		t.Errorf("launch start = %d, want between %d and %d", windows[0].startMS, before, after)
	}
}

// The binary's own shape: the executable is named codex, whatever its argv says. This is the
// half of the real chain (node wrapper, then the binary) that the wrapper fixture cannot prove.
func TestCodexStartWindowsFindsABinaryNamedCodex(t *testing.T) {
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	pid := fakeCodexBinary(t, cwd)
	if windows := codexStartWindows(pid); len(windows) != 1 || windows[0].cwd != cwd {
		t.Errorf("a binary named codex was read as %+v, want one launch in %q", windows, cwd)
	}
}

// The whole chain as this machine's installs present it: the wrapper process names the codex
// script (the argv rule's half), and the script starts the real binary as a child (the exe
// rule's half) — one launch, two codex processes, and the tree walk must find both.
func TestCodexStartWindowsFindsBothHalvesOfTheChain(t *testing.T) {
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep on this machine")
	}
	body, err := os.ReadFile(sleep)
	if err != nil {
		t.Skip("cannot read the sleep binary")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "wrapped", "bin", "codex")
	binary := filepath.Join(dir, "vendor", "bin", "codex")
	for _, path := range []string{script, binary} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n\"$1\" 60 &\nwait\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.WriteFile(binary, body, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	cmd := exec.Command("bash", script, binary)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the chain: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(3 * time.Second)
	var windows []codexWindow
	for {
		windows = codexStartWindows(cmd.Process.Pid)
		if len(windows) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(windows) != 2 {
		t.Fatalf("the chain read as %+v, want both its codex processes", windows)
	}
	for _, w := range windows {
		if w.cwd != cwd {
			t.Errorf("launch directory = %q, want %q", w.cwd, cwd)
		}
	}
}

func TestCodexStartWindowsIgnoresAMentionOfCodex(t *testing.T) {
	cwd := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	pid := fakeNonCodexProcess(t, cwd)
	if windows := codexStartWindows(pid); len(windows) != 0 {
		t.Errorf("a process that only mentions a codex path was read as %+v", windows)
	}
}

func TestCodexStartWindowsIgnoresAPaneWithNoCodex(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	if windows := codexStartWindows(cmd.Process.Pid); len(windows) != 0 {
		t.Errorf("a pane running sleep was read as %+v", windows)
	}
}
