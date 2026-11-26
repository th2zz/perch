package herdr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Harness struct {
	Kind       string `json:"kind"`
	Executable string `json:"executable"`
	Path       string `json:"path"`
}

var knownHarnesses = []Harness{
	{Kind: "pi", Executable: "pi"},
	{Kind: "claude", Executable: "claude"},
	{Kind: "codex", Executable: "codex"},
	{Kind: "cursor", Executable: "cursor"},
	{Kind: "devin", Executable: "devin"},
	{Kind: "agy", Executable: "agy"},
	{Kind: "cline", Executable: "cline"},
	{Kind: "omp", Executable: "omp"},
	{Kind: "mastracode", Executable: "mastracode"},
	{Kind: "opencode", Executable: "opencode"},
	{Kind: "copilot", Executable: "copilot"},
	{Kind: "kimi", Executable: "kimi"},
	{Kind: "kiro", Executable: "kiro"},
	{Kind: "droid", Executable: "droid"},
	{Kind: "amp", Executable: "amp"},
	{Kind: "grok", Executable: "grok"},
	{Kind: "hermes", Executable: "hermes"},
	{Kind: "kilo", Executable: "kilo"},
	{Kind: "qodercli", Executable: "qodercli"},
	{Kind: "qwen", Executable: "qwen"},
	{Kind: "letta", Executable: "letta"},
	{Kind: "maki", Executable: "maki"},
	{Kind: "muse", Executable: "muse"},
	// dsh-tui is started by typing its CLI launcher into a pane, because herdr's agent-start kind
	// list does not include it. herdr still detects the running process as agent "dsh-tui".
	{Kind: "dsh-tui", Executable: "dsh-tui"},
}

// StartsInPane reports whether this harness must be launched by running its own command in a pane
// instead of through herdr's agent.start.
func StartsInPane(kind string) bool {
	return kind == "dsh-tui"
}

// DetectedAgent is the agent name herdr reports once a pane-launched harness is running.
func DetectedAgent(kind string) string {
	return kind
}

func AvailableHarnesses() []Harness {
	return AvailableHarnessesIn(os.Getenv("PATH"))
}

func AvailableHarnessesIn(pathEnv string) []Harness {
	out := make([]Harness, 0, len(knownHarnesses))
	for _, harness := range knownHarnesses {
		path, ok := lookPathIn(pathEnv, harness.Executable)
		if !ok {
			continue
		}
		harness.Path = path
		out = append(out, harness)
	}
	return out
}

func HarnessFor(kind string) (Harness, error) {
	return HarnessForIn(os.Getenv("PATH"), kind)
}

func HarnessForIn(pathEnv, kind string) (Harness, error) {
	for _, harness := range knownHarnesses {
		if harness.Kind != kind {
			continue
		}
		path, ok := lookPathIn(pathEnv, harness.Executable)
		if !ok {
			return Harness{}, fmt.Errorf("%s is not installed on this computer", kind)
		}
		harness.Path = path
		return harness, nil
	}
	return Harness{}, fmt.Errorf("%s is not a supported harness", kind)
}

func lookPathIn(pathEnv, name string) (string, bool) {
	if pathEnv == "" {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
		return "", false
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, true
		}
	}
	return "", false
}

func CleanAgentArgs(args []string) ([]string, error) {
	if len(args) > 64 {
		return nil, fmt.Errorf("a session can take at most 64 arguments")
	}
	total := 0
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return nil, fmt.Errorf("arguments cannot contain control characters")
		}
		total += len(arg)
		if total > 4096 {
			return nil, fmt.Errorf("the arguments are too long")
		}
		clean = append(clean, arg)
	}
	return clean, nil
}
