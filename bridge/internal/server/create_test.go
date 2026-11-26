package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeHarnessOnPath(t *testing.T, kind string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, kind)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake harness: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

func TestCreateProjectCreatesWorkspaceAndStartsHarness(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "wX:p1", "workspace_id": "wX", "tab_id": "wX:t1",
		"focused": true, "agent": "codex", "agent_status": "idle", "cwd": "/home",
	})
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"my project","cwd":"/tmp","kind":"codex","name":"probe","args":["--model","gpt 5"]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if body["workspace_id"] != "wN" || body["pane_id"] != "wN:p1" {
		t.Fatalf("created %v, want workspace wN and pane wN:p1", body)
	}
	if !strings.Contains(body["equivalent"], "workspace create") ||
		!strings.Contains(body["equivalent"], "agent start") {
		t.Errorf("equivalent = %q, want both create and start commands", body["equivalent"])
	}
	if !strings.Contains(body["equivalent"], "'my project'") ||
		!strings.Contains(body["equivalent"], "'gpt 5'") {
		t.Errorf("equivalent = %q, want arguments with spaces quoted", body["equivalent"])
	}
	methods := fake.methods()
	if !contains(methods, "workspace.create") || !contains(methods, "agent.start") {
		t.Errorf("methods = %v, want workspace.create and agent.start", methods)
	}
	if contains(methods, "pane.focus") {
		t.Errorf("creating a project moved the person's cursor: %v", methods)
	}
	if focus := fake.paramsFor("workspace.create")["focus"]; focus != false {
		t.Errorf("workspace.create focus = %v, want false", focus)
	}
	if env := fake.paramsFor("workspace.create")["env"]; env != nil {
		t.Errorf("workspace.create env = %v, want none when the harness is on the bridge PATH", env)
	}
	if cwd := fake.paramsFor("workspace.create")["cwd"]; cwd != "/tmp" {
		t.Errorf("workspace.create cwd = %v, want the folder the phone chose (/tmp)", cwd)
	}
}

func TestCreateProjectRefusesAnInvalidFolderBeforeCreatingAnything(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)
	missing := filepath.Join(t.TempDir(), "not-here")
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	for _, tc := range []struct {
		name string
		cwd  string
		want string
	}{
		{"missing", missing, "does not exist"},
		{"file", file, "not a folder"},
		{"blank", "", "working folder is required"},
		{"relative", "relative/path", "full folder path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
				fmt.Sprintf(
					`{"label":"demo","cwd":%q,"kind":"codex","name":"probe"}`,
					tc.cwd,
				))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %v, want 400", status, body)
			}
			if !strings.Contains(body["error"], tc.want) {
				t.Errorf("error = %q, want it to name %q", body["error"], tc.want)
			}
			if body["fix"] == "" {
				t.Error("a refused project creation must tell the user what to do next")
			}
			if contains(fake.methods(), "workspace.create") {
				t.Errorf("an invalid folder still created a workspace: %v", fake.methods())
			}
		})
	}
}

func TestCreateProjectRefusesAnUnknownHarnessBeforeCreatingAnything(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	status, _ := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"not-a-harness","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if contains(fake.methods(), "workspace.create") {
		t.Errorf("an unknown harness still created a workspace: %v", fake.methods())
	}
}

func TestCreateProjectCleansUpWhenTheHarnessWillNotStart(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	fake.fail("agent.start", "agent_start_failed", "bad arguments")
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe","args":["--bad"]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !contains(fake.methods(), "workspace.close") {
		t.Errorf("a failed start left the new workspace behind: %v", fake.methods())
	}
}

func TestCreateProjectUnsticksAShellThatNeverReachesItsPrompt(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	fake.busyAgentStart(4)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !contains(fake.methods(), "pane.send_keys") {
		t.Errorf("a stuck shell was never interrupted: %v", fake.methods())
	}
}

func TestCreateProjectCleansUpAfterTheRequestIsCanceled(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	fake.delay("agent.start", 300*time.Millisecond)
	_, core := newTestServerWithSocket(t, socket)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := core.CreateProject(
		ctx, "local", "demo", "/tmp", "codex", "probe", nil,
	); err == nil {
		t.Fatal("a canceled create succeeded")
	}
	if !contains(fake.methods(), "workspace.close") {
		t.Errorf("a canceled create left its workspace behind: %v", fake.methods())
	}
}

func TestCreateProjectFailsWhenTheHarnessDoesNotStayRunning(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	fake.suppressAgentStart()
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !contains(fake.methods(), "workspace.close") {
		t.Errorf("a harness that never appeared left its workspace behind: %v", fake.methods())
	}
}

func TestCreateProjectFailsWhenTheHarnessExitsImmediately(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	fake.exitAgentAfter(300 * time.Millisecond)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !strings.Contains(body["error"], "did not stay running") {
		t.Errorf("error = %q, want the harness exit to be named", body["error"])
	}
	if !contains(fake.methods(), "workspace.close") {
		t.Errorf("an immediately exiting harness left its workspace behind: %v", fake.methods())
	}
}

func TestCreateSessionCreatesATabInTheProject(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "wN:p1", "workspace_id": "wN", "tab_id": "wN:t1", "cwd": "/tmp",
		"focused": true, "agent": "codex", "agent_status": "idle",
	})
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/sessions", testToken,
		`{"workspace_id":"wN","kind":"codex","name":"second"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if body["tab_id"] != "wN:t2" || body["pane_id"] != "wN:p2" {
		t.Fatalf("created %v, want tab wN:t2 and pane wN:p2", body)
	}
	if !contains(fake.methods(), "tab.create") || !contains(fake.methods(), "agent.start") {
		t.Errorf("methods = %v, want tab.create and agent.start", fake.methods())
	}
	if contains(fake.methods(), "pane.focus") {
		t.Errorf("creating a session moved the person's cursor: %v", fake.methods())
	}
	if focus := fake.paramsFor("tab.create")["focus"]; focus != false {
		t.Errorf("tab.create focus = %v, want false", focus)
	}
	if cwd := fake.paramsFor("tab.create")["cwd"]; cwd != "/tmp" {
		t.Errorf("tab.create cwd = %v, want the project's own folder (/tmp)", cwd)
	}
}

func TestCreateSessionLooksPastAPaneThatCannotNameItsFolder(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t,
		map[string]any{
			"pane_id": "wN:p1", "workspace_id": "wN", "tab_id": "wN:t1",
			"cwd": "", "focused": true, "agent": "codex", "agent_status": "idle",
		},
		map[string]any{
			"pane_id": "wN:p2", "workspace_id": "wN", "tab_id": "wN:t1",
			"cwd": "/tmp", "focused": false, "agent": "codex", "agent_status": "idle",
		},
	)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/sessions", testToken,
		`{"workspace_id":"wN","kind":"codex","name":"third"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v, want 200", status, body)
	}
	if cwd := fake.paramsFor("tab.create")["cwd"]; cwd != "/tmp" {
		t.Errorf("tab.create cwd = %v, want the other pane's folder (/tmp)", cwd)
	}
}

func TestCreateSessionRefusesAProjectFolderThatIsGoneBeforeCreatingATab(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	gone := filepath.Join(t.TempDir(), "gone")
	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "wN:p1", "workspace_id": "wN", "tab_id": "wN:t1",
		"cwd": gone, "focused": true, "agent": "codex", "agent_status": "idle",
	})
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/sessions", testToken,
		`{"workspace_id":"wN","kind":"codex","name":"second"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v, want 400", status, body)
	}
	if !strings.Contains(body["error"], "working folder is not usable") {
		t.Errorf("error = %q, want the project's missing folder named", body["error"])
	}
	if body["fix"] == "" {
		t.Error("a refused session must tell the user what to do next")
	}
	if contains(fake.methods(), "tab.create") {
		t.Errorf("a session in a missing folder still created a tab: %v", fake.methods())
	}
}

func TestCreateSessionRefusesWhenTheProjectFolderIsUnknown(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	fake, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/sessions", testToken,
		`{"workspace_id":"wN","kind":"codex","name":"second"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v, want 400", status, body)
	}
	if !strings.Contains(body["error"], "working folder") {
		t.Errorf("error = %q, want it to say the project has no folder to inherit", body["error"])
	}
	if contains(fake.methods(), "tab.create") {
		t.Errorf("a session with no folder still created a tab: %v", fake.methods())
	}
}

func readyDshHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	profile := filepath.Join(home, "profiles", "dsh-tui")
	plugin := filepath.Join(
		profile, "node_modules", "@deepseek-harness-tui", "dsh-tui", "package.json",
	)
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatalf("make dsh profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profile, "cordis.patch.yml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatalf("write dsh profile: %v", err)
	}
	if err := os.WriteFile(plugin, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write dsh plugin: %v", err)
	}
}

func TestCreateProjectStartsDshInItsCliPane(t *testing.T) {
	fakeHarnessOnPath(t, "dsh-tui")
	readyDshHome(t)
	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "wX:p1", "workspace_id": "wX", "tab_id": "wX:t1", "focused": true,
	})
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"dsh project","cwd":"/tmp","kind":"dsh-tui","name":"probe","args":["--resume","abc def"]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	methods := fake.methods()
	if contains(methods, "agent.start") {
		t.Errorf("dsh was sent through agent.start: %v", methods)
	}
	if !contains(methods, "pane.send_input") {
		t.Fatalf("dsh was not started in its pane: %v", methods)
	}
	text, _ := fake.paramsFor("pane.send_input")["text"].(string)
	if !strings.Contains(text, "dsh-tui") || !strings.Contains(text, "'abc def'") {
		t.Errorf("pane command = %q, want dsh-tui and the quoted argument", text)
	}
	if !strings.Contains(body["equivalent"], "pane run") ||
		!strings.Contains(body["equivalent"], "dsh-tui") {
		t.Errorf("equivalent = %q, want the pane.run command", body["equivalent"])
	}
}

func TestCreateProjectRefusesDshWithoutItsCliProfile(t *testing.T) {
	fakeHarnessOnPath(t, "dsh-tui")
	t.Setenv("DSH_HOME", t.TempDir())
	fake, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"dsh project","cwd":"/tmp","kind":"dsh-tui","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !strings.Contains(body["error"], "profile") {
		t.Errorf("error = %q, want the profile problem", body["error"])
	}
	if !strings.Contains(body["fix"], "dsh-tui") {
		t.Errorf("fix = %q, want the profile repair instruction", body["fix"])
	}
	if contains(fake.methods(), "workspace.create") {
		t.Errorf("a missing dsh profile still created a workspace: %v", fake.methods())
	}
}

func TestCreateProjectDoesNotFakeDshOnboardingWhenReadinessFails(t *testing.T) {
	cases := []struct {
		name   string
		script string
		setup  func(t *testing.T)
	}{
		{
			name:   "missing profile",
			script: "#!/bin/sh\nexit 0\n",
			setup:  func(t *testing.T) { t.Setenv("DSH_HOME", t.TempDir()) },
		},
		{
			name:   "missing plugin",
			script: "#!/bin/sh\nexit 0\n",
			setup: func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("DSH_HOME", home)
				profile := filepath.Join(home, "profiles", "dsh-tui")
				if err := os.MkdirAll(profile, 0o755); err != nil {
					t.Fatalf("make profile: %v", err)
				}
				if err := os.WriteFile(
					filepath.Join(profile, "cordis.patch.yml"), []byte("[]\n"), 0o644,
				); err != nil {
					t.Fatalf("write profile: %v", err)
				}
			},
		},
		{
			name:   "doctor fails",
			script: "#!/bin/sh\necho '✗ DEEPSEEK_API_KEY: missing'\nexit 1\n",
			setup:  readyDshHome,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "dsh-tui")
			if err := os.WriteFile(path, []byte(tc.script), 0o755); err != nil {
				t.Fatalf("write dsh-tui: %v", err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
			tc.setup(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			fake, socket := startFakeHerdr(t)
			ts, _ := newTestServerWithSocket(t, socket)

			status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
				`{"label":"dsh project","cwd":"/tmp","kind":"dsh-tui","name":"probe"}`)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %v", status, body)
			}
			if _, err := os.Stat(filepath.Join(home, ".dsh-tui", "onboarding.json")); !os.IsNotExist(err) {
				t.Errorf("readiness failed but onboarding was marked complete: %v", err)
			}
			if contains(fake.methods(), "workspace.create") {
				t.Errorf("an unready dsh still created a workspace: %v", fake.methods())
			}
		})
	}
}

func unreadyHarnessOnPath(t *testing.T, kind string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, kind)
	script := "#!/bin/sh\necho 'Not logged in'\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write unready harness: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")
}

func TestCreateProjectRefusesAnUnreadyHarnessBeforeCreatingAnything(t *testing.T) {
	unreadyHarnessOnPath(t, "codex")
	t.Setenv("CODEX_HOME", t.TempDir())
	fake, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !strings.Contains(body["error"], "not signed in") {
		t.Errorf("error = %q, want the readiness reason", body["error"])
	}
	if !strings.Contains(body["fix"], "codex login") {
		t.Errorf("fix = %q, want the login instruction", body["fix"])
	}
	if contains(fake.methods(), "workspace.create") {
		t.Errorf("an unready harness still created a workspace: %v", fake.methods())
	}
}

func TestCreateSessionRefusesAnUnreadyHarnessBeforeCreatingAnything(t *testing.T) {
	unreadyHarnessOnPath(t, "codex")
	t.Setenv("CODEX_HOME", t.TempDir())
	fake, socket := startFakeHerdr(t, map[string]any{
		"pane_id": "wX:p1", "workspace_id": "wX", "tab_id": "wX:t1", "focused": true,
	})
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/sessions", testToken,
		`{"workspace_id":"wX","kind":"codex","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !strings.Contains(body["fix"], "codex login") {
		t.Errorf("fix = %q, want the login instruction", body["fix"])
	}
	if contains(fake.methods(), "tab.create") {
		t.Errorf("an unready harness still created a session: %v", fake.methods())
	}
}

func TestCreateProjectReportsDshThatExitsBeforeItIsReady(t *testing.T) {
	fakeHarnessOnPath(t, "dsh-tui")
	readyDshHome(t)
	fake, socket := startFakeHerdr(t)
	fake.exitAgentAfter(300 * time.Millisecond)
	ts, _ := newTestServerWithSocket(t, socket)

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"dsh project","cwd":"/tmp","kind":"dsh-tui","name":"probe"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !strings.Contains(body["error"], "stopped before it was ready") {
		t.Errorf("error = %q, want the exit reported", body["error"])
	}
	if !strings.Contains(body["error"], "what now?") {
		t.Errorf("error = %q, want the pane output included", body["error"])
	}
	if !contains(fake.methods(), "workspace.close") {
		t.Errorf("an exiting dsh left its workspace behind: %v", fake.methods())
	}
}

func TestHarnessLookupMergesEveryPanePath(t *testing.T) {
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatalf("find sleep: %v", err)
	}
	bridgePath := t.TempDir()
	focusedPath := t.TempDir()
	otherPath := t.TempDir()
	t.Setenv("PATH", bridgePath)
	writeHarness(t, otherPath, "codex")

	focusedPID := sleepWithPath(t, sleepBin, focusedPath)
	otherPID := sleepWithPath(t, sleepBin, otherPath)
	fake, socket := startFakeHerdr(t,
		map[string]any{"pane_id": "w1:p1", "workspace_id": "w1", "tab_id": "w1:t1", "focused": true},
		map[string]any{"pane_id": "w2:p1", "workspace_id": "w2", "tab_id": "w2:t1", "focused": false},
	)
	fake.setProcessInfoFor("w1:p1", map[string]any{"pid": focusedPID})
	fake.setProcessInfoFor("w2:p1", map[string]any{"pid": otherPID})
	ts, _ := newTestServerWithSocket(t, socket)

	var response struct {
		Harnesses []struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		} `json:"harnesses"`
	}
	if err := json.Unmarshal(getJSON(t, ts, "/v1/harnesses", testToken), &response); err != nil {
		t.Fatalf("decode harnesses: %v", err)
	}
	if len(response.Harnesses) != 1 || response.Harnesses[0].Kind != "codex" {
		t.Fatalf("harnesses = %+v, want codex from the second pane", response.Harnesses)
	}
	if !strings.HasPrefix(response.Harnesses[0].Path, otherPath) {
		t.Errorf("codex path = %q, want it under %q", response.Harnesses[0].Path, otherPath)
	}

	status, body := call(t, ts, http.MethodPost, "/v1/projects", testToken,
		`{"label":"demo","cwd":"/tmp","kind":"codex","name":"probe"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	env, _ := fake.paramsFor("workspace.create")["env"].(map[string]any)
	envPath, _ := env["PATH"].(string)
	if !strings.Contains(envPath, otherPath) {
		t.Errorf("workspace.create env = %v, want PATH including %q", env, otherPath)
	}
	if !strings.Contains(body["equivalent"], "--env PATH=") {
		t.Errorf("equivalent = %q, want the PATH override it used", body["equivalent"])
	}
}

func writeHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755,
	); err != nil {
		t.Fatalf("write fake harness: %v", err)
	}
}

func sleepWithPath(t *testing.T, program, path string) int {
	t.Helper()
	return sleepWithEnv(t, program, []string{"PATH=" + path})
}

func sleepWithEnv(t *testing.T, program string, env []string) int {
	t.Helper()
	cmd := exec.Command(program, "30")
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	for i := 0; i < 100; i++ {
		data, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "environ"))
		if err == nil && len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return pid
}

func TestHarnessesReportsOnlyInstalledCommands(t *testing.T) {
	fakeHarnessOnPath(t, "codex")
	_, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	var response struct {
		Harnesses []struct {
			Kind       string `json:"kind"`
			Ready      bool   `json:"ready"`
			NeedsSetup bool   `json:"needs_setup"`
		} `json:"harnesses"`
		DefaultCwd string `json:"default_cwd"`
	}
	if err := json.Unmarshal(getJSON(t, ts, "/v1/harnesses", testToken), &response); err != nil {
		t.Fatalf("decode harnesses: %v", err)
	}
	if response.DefaultCwd == "" {
		t.Errorf("no default folder was reported")
	}
	seen := map[string]bool{}
	for _, harness := range response.Harnesses {
		seen[harness.Kind] = true
	}
	if !seen["codex"] {
		t.Errorf("installed codex was not reported: %+v", response.Harnesses)
	}
	if seen["claude"] {
		t.Errorf("claude is not on this test PATH but was reported: %+v", response.Harnesses)
	}
	for _, harness := range response.Harnesses {
		if harness.Kind == "codex" && (!harness.Ready || harness.NeedsSetup) {
			t.Errorf("a fake codex that exits 0 was not ready: %+v", harness)
		}
	}
}

func TestHarnessesReportsWhyAHarnessNeedsSetup(t *testing.T) {
	unreadyHarnessOnPath(t, "codex")
	t.Setenv("CODEX_HOME", t.TempDir())
	_, socket := startFakeHerdr(t)
	ts, _ := newTestServerWithSocket(t, socket)

	var response struct {
		Harnesses []struct {
			Kind       string `json:"kind"`
			Ready      bool   `json:"ready"`
			NeedsSetup bool   `json:"needs_setup"`
			Reason     string `json:"reason"`
			Fix        string `json:"fix"`
		} `json:"harnesses"`
	}
	if err := json.Unmarshal(getJSON(t, ts, "/v1/harnesses", testToken), &response); err != nil {
		t.Fatalf("decode harnesses: %v", err)
	}
	if len(response.Harnesses) != 1 {
		t.Fatalf("harnesses = %+v, want only codex", response.Harnesses)
	}
	harness := response.Harnesses[0]
	if harness.Kind != "codex" || harness.Ready || !harness.NeedsSetup {
		t.Errorf("harness = %+v, want codex needing setup", harness)
	}
	if !strings.Contains(harness.Reason, "not signed in") {
		t.Errorf("reason = %q, want the readiness reason", harness.Reason)
	}
	if !strings.Contains(harness.Fix, "codex login") {
		t.Errorf("fix = %q, want the login instruction", harness.Fix)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
