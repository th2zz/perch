package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestConversationEndpointReadsAnAppServerRowFromBeforeTheLaunch(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)

	sessionID := "01a147f7-8415-7f20-80b2-ff97735ddd1d"
	scriptDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	script := filepath.Join(scriptDir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("write codex: %v", err)
	}
	process := exec.Command("bash", script)
	process.Dir = cwd
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := process.Start(); err != nil {
		t.Fatalf("start codex: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
		_, _ = process.Process.Wait()
	})

	sessions := filepath.Join(home, ".codex", "sessions", "2026", "10", "17")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	rollout := filepath.Join(sessions, "rollout-2026-10-17T12-46-05-"+sessionID+".jsonl")
	record := strings.Join([]string{
		fmt.Sprintf(`{"type":"session_meta","payload":{"session_id":%q,"cwd":%q,"originator":"codex-tui"}}`, sessionID, cwd),
		`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":[{"type":"text","text":"hello"}]}}}`,
		`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","content":[{"type":"Text","text":"world"}],"phase":"final_answer"}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(rollout, []byte(record), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	tool, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not available")
	}
	launch := time.Now().UnixMilli()
	db := filepath.Join(home, ".codex", "state_5.sqlite")
	stmts := fmt.Sprintf(
		`create table threads (id text primary key, rollout_path text not null, created_at_ms integer not null, `+
			`updated_at_ms integer, cwd text not null, originator text not null);`+
			`insert into threads values ('%s','%s',%d,%d,'%s','codex-tui');`,
		sessionID, rollout, launch-5_000, launch+1_000, cwd)
	if out, err := exec.Command(tool, db, stmts).CombinedOutput(); err != nil {
		t.Fatalf("build state database: %v: %s", err, out)
	}

	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1",
		"agent": "codex", "agent_status": "idle", "cwd": cwd, "foreground_cwd": cwd,
	})
	fake.setProcessInfo(map[string]any{
		"argv": []any{"bash", script}, "pid": process.Process.Pid, "cwd": cwd, "name": "codex",
	})
	ts, _ := newTestServerWithSocket(t, socket)

	var response struct {
		Readable bool `json:"readable"`
		Messages []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(
		getJSON(t, ts, "/v1/session/conversation?pane_id=w1:p1", testToken), &response,
	); err != nil {
		t.Fatalf("decode conversation: %v", err)
	}
	if !response.Readable {
		t.Fatal("the endpoint reported readable=false")
	}
	if len(response.Messages) != 2 || response.Messages[0].Text != "hello" ||
		response.Messages[1].Text != "world" {
		t.Fatalf("messages = %+v", response.Messages)
	}
}
