package herdr

import (
	"strings"
	"testing"
)

func TestFormatShellQuotesArgumentsThatNeedIt(t *testing.T) {
	got := FormatShell([]string{"workspace", "create", "--label", "my project", "--", "a'b"})
	if !strings.Contains(got, "--label 'my project'") {
		t.Errorf("label was not quoted: %s", got)
	}
	if !strings.Contains(got, `'a'\''b'`) {
		t.Errorf("embedded quote was not escaped: %s", got)
	}
	if strings.Contains(got, "'--label'") {
		t.Errorf("plain flags should stay readable: %s", got)
	}
}

func TestCreateWorkspaceCommandCarriesAFalseFocusAndSortedEnv(t *testing.T) {
	got := CreateWorkspaceCommand(
		"/tmp", "demo", map[string]string{"PATH": "/pane/bin", "A": "1"}, false,
	)
	want := []string{
		"workspace", "create", "--cwd", "/tmp", "--label", "demo",
		"--env", "A=1", "--env", "PATH=/pane/bin", "--no-focus",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("command = %v, want %v", got, want)
	}
}
