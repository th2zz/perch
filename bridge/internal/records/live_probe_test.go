package records

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestLivePanesProbe(t *testing.T) {
	if os.Getenv("PERCH_LIVE_PROBE") == "" {
		t.Skip("set PERCH_LIVE_PROBE=1")
	}
	home, _ := os.UserHomeDir()
	for _, pane := range []string{"wA:p1", "w2:p1"} {
		out, err := exec.Command("herdr", "pane", "process-info", "--pane", pane).Output()
		if err != nil {
			t.Logf("%s: %v", pane, err)
			continue
		}
		var envelope struct {
			Result struct {
				ProcessInfo struct {
					ForegroundProcesses []struct {
						Argv []string `json:"argv"`
						PID  int      `json:"pid"`
						Cwd  string   `json:"cwd"`
					} `json:"foreground_processes"`
				} `json:"process_info"`
			} `json:"result"`
		}
		if json.Unmarshal(out, &envelope) != nil {
			continue
		}
		for _, p := range envelope.Result.ProcessInfo.ForegroundProcesses {
			where := Where{PaneID: pane, CWD: p.Cwd, Argv: p.Argv, PID: p.PID}
			if facts, ok := Read("codex", home, where); ok {
				fmt.Printf("%s argv=%v\n  %s\n", pane, p.Argv, mustJSON(facts))
			} else if facts, ok := Read("claude", home, where); ok {
				fmt.Printf("%s argv=%v (claude)\n  %s\n", pane, p.Argv, mustJSON(facts))
			} else {
				fmt.Printf("%s argv=%v -> no facts\n", pane, p.Argv)
			}
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.MarshalIndent(v, "  ", "  ")
	return string(b)
}
