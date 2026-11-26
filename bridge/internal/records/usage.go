package records

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Facts is what a harness's own record says about its session.
//
// Everything is optional: a harness that does not write it, or a file we cannot read, leaves
// the field empty and the app shows nothing for it. Missing is honest here — the alternative
// is inventing numbers about somebody's spending.
type Facts struct {
	// Harness is the kind herdr reported ("codex", "claude"), so the app can label the panel.
	Harness string `json:"harness,omitempty"`
	// (Readable, below, distinguishes "we cannot read this agent" from "it says nothing yet".)
	Model string `json:"model,omitempty"`
	// Effort is the reasoning effort the harness is configured with, when it records one.
	Effort string `json:"effort,omitempty"`
	// ContextUsed is how much of the model's window the current session occupies, as the
	// harness itself counts it, and ContextWindow is the size of that window. Percentages are
	// computed by the app from the two, so a harness that reports neither shows neither.
	ContextUsed   int64 `json:"context_used,omitempty"`
	ContextWindow int64 `json:"context_window,omitempty"`
	// TotalTokens is the session's whole spend, and CachedTokens how much of the input came
	// from the provider's cache — which is what makes a long session affordable.
	TotalTokens  int64 `json:"total_tokens,omitempty"`
	CachedTokens int64 `json:"cached_tokens,omitempty"`
	// Access is the permission mode the harness is running under, in words a person can act
	// on ("full access, no approvals"), taken from the harness's own policy fields.
	Access string `json:"access,omitempty"`
	// Goal and GoalStatus are the session's goal and where it stands, from the harness's own
	// goal store. Both are empty for a session that has no goal.
	Goal       string `json:"goal,omitempty"`
	GoalStatus string `json:"goal_status,omitempty"`
	// SessionID is the harness's own identifier for the session, which is how everything above
	// was found. Shown nowhere; it is here because a support conversation needs it.
	SessionID string `json:"session_id,omitempty"`

	// Readable says whether Perch knows how to read this harness's records *at all*, which is a
	// different fact from "it recorded nothing" and has to stay different: a user staring at an
	// empty panel deserves to know whether we have not learned their agent yet or whether their
	// agent has simply not written anything down. Merging the two into one empty panel is the
	// kind of small dishonesty that makes people stop believing the rest of the screen.
	Readable bool `json:"readable"`
}

// Where is what herdr told us about the process running in the pane.
//
// It comes from `herdr pane process-info`, which is the piece that makes all of this possible
// without guessing: the argv of the harness process carries its session identity for codex,
// and the pid is enough to find the file a harness holds open.
type Where struct {
	PaneID string
	CWD    string
	Argv   []string
	PID    int
	// SessionID is the identity the harness registered with herdr itself (`agent_session`,
	// kind "id"): the first thing that names the session, and the only one that names it for a
	// pane the harness's argv never named. It is a cache — the reader refuses it when the
	// pane's process tree positively holds a different record open (codexRollout in this
	// package).
	SessionID string
	// Title is the pane's own terminal title. A resumed codex session keeps the title the
	// session had when its process restarted, which is how an old row is matched to the right
	// pane when the launch window cannot do it.
	Title string
	// PreferredSessionID is the bridge's persisted identity for this pane. It is
	// consulted only after the fresh sources fail, and only when its record still
	// exists.
	PreferredSessionID string
}

func Read(harness, home string, w Where) (Facts, bool) {
	switch harness {
	case "codex":
		facts, _ := codexFacts(home, w)
		facts.Readable = true
		return facts, true
	case "claude":
		facts, _ := claudeFacts(home, w)
		facts.Readable = true
		return facts, true
	default:
		return Facts{}, false
	}
}

// ---------------------------------------------------------------- codex

// codexSessionID finds the session identifier codex names on its own command line.
//
// A resumed session reads `node …/codex resume 01a128ff-4c2c-…`, and that identifier is the
// same one its record file is named after — so this single argument removes every guess about
// *which* of a machine's codex sessions the pane is running. (A machine here has eight of them
// open at once, several in the same directory, which is why cwd and modification time were
// never going to be enough.)
// Two patterns for the same identity, because the two places it is found say different things about
// it. On a command line the id *is* the argument, so the match is anchored: an id embedded in
// something longer is a different argument. In a record's file name it is the tail of a name that
// also carries the hour the session started — so that one is anchored at the end, and only there.
var (
	codexSessionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	codexRecordID  = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func sessionIDFromArgv(argv []string) string {
	for i := len(argv) - 1; i >= 0; i-- {
		if codexSessionID.MatchString(argv[i]) {
			return argv[i]
		}
	}
	return ""
}

func sessionIDFromRecordName(path string) string {
	return codexRecordID.FindString(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
}

// codexRollout names the record file codex is writing for one pane's session, and the session id it
// belongs to.
//
// Four sources, and they are not equals. What the pane's process tree holds open is the kernel's
// answer about the live session — and the only source that exists for a session that named itself
// nowhere. The identity herdr registered and the id on the command line are both *caches*: either
// can still name the session the pane used to run. So the identities are taken in the order they
// are trusted — registration, then command line — with two guards: when the two disagree the
// command line wins (it is read fresh with this very request while the registration is a mirror
// that lags), and a cache is refused when the kernel positively shows a *different* record open
// under the same pane. When the tree holds nothing (a tool running in the pane, a moment between
// writes, a machine where /proc cannot answer), the surviving cache is all there is and is taken
// as before. Only when no identity was named at all does the state database get asked (see
// codex_state.go) — it is an inference about a launch, so it must never overrule a name, and a
// name whose record was not found is a refusal, not a hole for the inference to fill. The id
// always comes off the file that will be read, so the goal lookup downstream is never handed an id
// that names no open record.
func codexRollout(home string, w Where) (path, id string) {
	held := recordUnder(w.PID, filepath.Join(home, ".codex", "sessions"))

	// The command line is this request's own fresh evidence; the registration is a cache. When
	// they disagree, the cache is stale — a pane resumed as B while herdr still remembers A
	// must not serve A's conversation — so the stale registration is dropped, not consulted.
	argvID := sessionIDFromArgv(w.Argv)
	candidates := []string{w.SessionID}
	if argvID != "" && argvID != w.SessionID {
		candidates = []string{argvID}
	} else if argvID != "" {
		candidates = append(candidates, argvID)
	}

	for _, candidate := range candidates {
		found := findCodexRollout(home, candidate)
		if found == "" {
			continue
		}
		if held != "" && !sameFile(found, held) {
			continue
		}
		if argvID == "" && w.SessionID != "" {
			if own, ownID := codexStateRolloutCached(home, w); own != "" && !sameFile(own, found) {
				return own, ownID
			}
		}
		if named := sessionIDFromRecordName(found); named != "" {
			candidate = named
		}
		return found, candidate
	}
	if held != "" {
		return held, sessionIDFromRecordName(held)
	}
	// The bridge's persisted identity is the next-strongest answer after the
	// kernel: it was learned from a successful read of this same pane, and the
	// title guard in core is what keeps it from surviving `/new`.
	if w.PreferredSessionID != "" {
		if found := findCodexRollout(home, w.PreferredSessionID); found != "" {
			return found, w.PreferredSessionID
		}
	}
	// A name was given and its record was not found: the pane's session is known to the
	// harness, and an inference about a launch must not fill the name's place. The state
	// database's row would be exactly such an inference — it pairs *a* session with the
	// launch, and the missing record is evidence the pair does not hold for this pane.
	if w.SessionID != "" || argvID != "" {
		return "", ""
	}
	// Nothing named the session and the pane's tree holds nothing. codex 0.163's managed
	// app-server is the process that holds the rollout, and it does not descend from the pane
	// (so the kernel cannot answer), while a pane whose SessionStart hook reported to the
	// daemon's stale pane named itself nowhere. What codex wrote down for this launch — its
	// state database row — is what is left, and it is asked strictly last so an inference about
	// a launch can never overrule a name the harness gave.
	if path, id := codexStateRolloutCached(home, w); path != "" {
		return path, id
	}
	return "", ""
}

type statePair struct {
	home      string
	pid       int
	sessionID string
	path      string
	id        string
	checked   time.Time
}

const statePairRetry = 5 * time.Second

var (
	statePairMu sync.Mutex
	statePairs  = map[string]statePair{}
)

func codexStateRolloutCached(home string, w Where) (string, string) {
	statePairMu.Lock()
	defer statePairMu.Unlock()
	if p, ok := statePairs[w.PaneID]; ok && p.home == home && p.pid == w.PID && p.sessionID == w.SessionID {
		if p.path != "" || time.Since(p.checked) < statePairRetry {
			return p.path, p.id
		}
	}
	path, id := codexStateRollout(home, w)
	statePairs[w.PaneID] = statePair{home: home, pid: w.PID, sessionID: w.SessionID, path: path, id: id, checked: time.Now()}
	return path, id
}

// sameFile reports whether two paths are the same file. Their spellings can differ — /proc answers
// with canonical paths while the home we were handed may go through a symlink — so the question is
// asked of the file, not of the string.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// codexFacts reads the record codex is writing for this session.
func codexFacts(home string, w Where) (Facts, bool) {
	path, id := codexRollout(home, w)
	if path == "" {
		return Facts{}, false
	}
	facts := Facts{Harness: "codex", SessionID: id}

	// The head carries the turn's context (model, effort, permissions); the tail carries the
	// newest spend. Neither is read whole: this session's record is 51 MB after two days.
	if context := lastJSONFromHead(rawHead(path, 512<<10), "turn_context"); context != nil {
		var payload struct {
			Model          string `json:"model"`
			Effort         string `json:"effort"`
			ApprovalPolicy string `json:"approval_policy"`
			SandboxPolicy  struct {
				Type string `json:"type"`
			} `json:"sandbox_policy"`
		}
		if json.Unmarshal(context, &payload) == nil {
			facts.Model = payload.Model
			facts.Effort = payload.Effort
			facts.Access = accessWords(payload.ApprovalPolicy, payload.SandboxPolicy.Type)
		}
	}
	// The spend and the window are two different records, and asking for both kinds at once
	// returns whichever came last in the file — which is how the first version of this reported
	// a model and a goal with no tokens at all.
	spend := lastJSONFromTail(path, "token_usage_record")
	if spend == nil {
		spend = lastJSONFromTail(path, "token_count")
	}
	facts.mergeCodexUsage(spend)
	// The window lives in its own record (an `event_msg` whose payload is a token count), and
	// a newer codex reports the context size without it — so it is read separately rather than
	// assumed to arrive with the spend.
	if facts.ContextWindow == 0 {
		facts.ContextWindow = lastContextWindow(path)
	}
	facts.Goal, facts.GoalStatus = codexGoal(home, facts.SessionID)
	if facts.Model == "" && facts.TotalTokens == 0 && facts.Goal == "" {
		return Facts{}, false
	}
	return facts, true
}

// mergeCodexUsage pulls the numbers out of whichever shape codex wrote them in.
//
// Two shapes exist in the same file, because the record grew: `token_usage_record` lines carry
// `usage`, and `event_msg` lines of type `token_count` carry `info`. Both hold the same three
// things under different names, and both are read rather than one being assumed.
func (f *Facts) mergeCodexUsage(raw json.RawMessage) {
	if raw == nil {
		return
	}
	var payload codexUsage
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	// Newer records: the session's spend under `thread_token_usage`, this turn's context under
	// `usage`. Older ones: everything in one `info` object, which is also the shape the status
	// line was printing.
	if payload.ThreadUsage.TotalTokens > 0 {
		f.TotalTokens = payload.ThreadUsage.TotalTokens
		f.CachedTokens = payload.ThreadUsage.CachedInputTokens
	} else if payload.Info != nil {
		f.TotalTokens = payload.Info.TotalTokenUsage.TotalTokens
		f.CachedTokens = payload.Info.TotalTokenUsage.CachedInputTokens
	}
	// "Context used" is what the next request would send. The cached count is part of the input
	// count, not additional to it — measured on a live session, where `total_tokens` equals
	// `input_tokens + output_tokens` exactly — so adding them would overstate the window by
	// about half.
	switch {
	case payload.Usage.InputTokens > 0:
		f.ContextUsed = payload.Usage.InputTokens
	case payload.Info != nil:
		f.ContextUsed = payload.Info.LastTokenUsage.InputTokens
	}
	if payload.ModelContextWindow > 0 {
		f.ContextWindow = payload.ModelContextWindow
	} else if payload.Info != nil && payload.Info.ModelContextWindow > 0 {
		f.ContextWindow = payload.Info.ModelContextWindow
	}
}

type codexUsage struct {
	// Newer shape.
	Usage       codexTokens `json:"usage"`
	TurnUsage   codexTokens `json:"turn_token_usage"`
	ThreadUsage codexTokens `json:"thread_token_usage"`
	// Older shape, and the flat window that newer records carry next to `usage`.
	Info               *codexInfo `json:"info"`
	ModelContextWindow int64      `json:"model_context_window"`
}

type codexInfo struct {
	TotalTokenUsage    codexTokens `json:"total_token_usage"`
	LastTokenUsage     codexTokens `json:"last_token_usage"`
	ModelContextWindow int64       `json:"model_context_window"`
}

type codexTokens struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

// accessWords says what the harness's permission fields mean in words a person can act on.
func accessWords(approval, sandbox string) string {
	var parts []string
	switch sandbox {
	case "danger-full-access":
		parts = append(parts, "full access")
	case "read-only":
		parts = append(parts, "read-only")
	case "workspace-write":
		parts = append(parts, "can write in the workspace")
	}
	switch approval {
	case "never":
		parts = append(parts, "no approvals asked")
	case "on-request":
		parts = append(parts, "asks before acting")
	}
	return strings.Join(parts, ", ")
}

// findCodexRollout finds the record file for a session id, newest day directories first.
func findCodexRollout(home, id string) string {
	// Only a whole session id may drive the search: the pattern is `*<id>*.jsonl`, so a
	// fragment ("rollout", a short prefix) would match some other session's record and serve it
	// as this pane's. The format is codex's own (a uuid), and nothing else is globbed.
	if !codexSessionID.MatchString(id) {
		return ""
	}
	root := filepath.Join(home, ".codex", "sessions")
	days, err := filepath.Glob(filepath.Join(root, "*", "*", "*"))
	if err != nil {
		return ""
	}
	// Newest first: a session id is unique, but the walk stops as soon as it is found.
	for i := len(days) - 1; i >= 0; i-- {
		if match, err := filepath.Glob(filepath.Join(days[i], "*"+id+"*.jsonl")); err == nil && len(match) > 0 {
			return match[0]
		}
	}
	return ""
}

// recordUnder finds the record a pane's process tree holds open under one directory, most recently
// written first.
//
// The tree, not just the process, is the point: an agent's foreground process is often a wrapper
// around the one that holds the record — codex 0.163's foreground pid is a node script whose
// grandchild daemon owns the rollout — so a walk that stops at the first process finds nothing at
// all. What the kernel says is open is the freshest fact available about which session a pane is
// running, and the only one for a session that named itself nowhere. Linux only — /proc is where it
// lives — and a machine that cannot answer reports nothing, which callers read as "no
// contradiction" rather than "nothing is open".
func recordUnder(pid int, under string) string {
	// The kernel's readlink answers with canonical paths, while `under` is whatever was joined
	// from the user's home path — which may itself contain symlinks (NixOS, Silverblue). Both
	// sides are compared in the same spelling so a symlinked home does not turn the answer into
	// "nothing is open".
	prefix := under
	if resolved, err := filepath.EvalSymlinks(under); err == nil {
		prefix = resolved
	}
	var best string
	var bestAt int64
	for _, p := range processTree(pid) {
		if found, at := newestFileUnder(p, prefix); found != "" && at > bestAt {
			best, bestAt = found, at
		}
	}
	return best
}

// newestFileUnder finds the most recently written file under one directory that a single process
// holds open, if any. The live session is the one being written; a process may hold several open.
func newestFileUnder(pid int, under string) (string, int64) {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}
	var best string
	var bestAt int64
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil || !strings.HasPrefix(target, under) || !strings.HasSuffix(target, ".jsonl") {
			continue
		}
		info, err := os.Stat(target)
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() > bestAt {
			best, bestAt = target, info.ModTime().UnixNano()
		}
	}
	return best, bestAt
}

// processChildren lists a process's direct children, from the kernel's own task tree.
func processChildren(pid int) []int {
	tasks, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "task"))
	if err != nil {
		return nil
	}
	var children []int
	for _, task := range tasks {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", task.Name(), "children"))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			if child, err := strconv.Atoi(field); err == nil && child > 0 {
				children = append(children, child)
			}
		}
	}
	return children
}

// codexGoal reads the goal for a session out of codex's own goal database.
//
// Best effort, and honestly so: the store is SQLite, and SQLite from Go without cgo means
// either a large dependency or the command-line tool. The tool is asked when it is present and
// silently skipped when it is not — a machine without it loses one line of a panel, never a
// feature.
func codexGoal(home, threadID string) (objective, status string) {
	if threadID == "" {
		return "", ""
	}
	db := filepath.Join(home, ".codex", "goals_1.sqlite")
	if _, err := os.Stat(db); err != nil {
		return "", ""
	}
	tool := sqliteTool()
	if tool == "" {
		return "", ""
	}
	// The id comes from codex's own record, and the quotes are doubled so a value can never
	// end the literal. Nothing else from the outside reaches this query.
	query := fmt.Sprintf("select status || '\t' || replace(objective, '\n', ' ') from thread_goals where thread_id = '%s' limit 1",
		strings.ReplaceAll(threadID, "'", "''"))
	ctx, cancel := context.WithTimeout(context.Background(), sqliteQueryBudget)
	defer cancel()
	out, err := querySQLite(ctx, tool, db, query)
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[1], parts[0]
}

// sqliteQueryBudget is how long any one sqlite3 question may take. Every call sits on the
// phone's polling path, and each database is shared with a live codex that can be holding a
// transaction over it — the two together are why a question that cannot be answered quickly is
// answered not at all.
const sqliteQueryBudget = 2 * time.Second

// querySQLite runs one read-only question through the sqlite3 command-line tool, under the
// caller's deadline.
//
// -readonly is not decoration: without it sqlite3 opens the file read-write, and every database
// here is being written by a live codex — the tool must be structurally unable to take part.
// The deadline is the other half: the tool is killed when it expires, so a database locked by a
// live codex can never pin a bridge request goroutine. WaitDelay closes it: a killed tool must
// not keep the caller waiting either, which it would if a process the tool started held the
// output pipe open past the kill.
func querySQLite(ctx context.Context, tool, db, query string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, "-readonly", db, query)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// sqliteTool finds a read-only SQLite CLI, if this machine has one.
func sqliteTool() string {
	if path, err := exec.LookPath("sqlite3"); err == nil {
		return path
	}
	// The Android SDK ships one and puts it in the user's own PATH when they are setting up a
	// device, which is exactly the machine this product is built on — but the bridge runs as a
	// service with a minimal environment, so the common location is checked explicitly.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(home, ".local", "opt", "android-sdk", "platform-tools", "sqlite3")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// ---------------------------------------------------------------- claude

// claudeFacts reads the transcript claude is writing for this session.
func claudeFacts(home string, w Where) (Facts, bool) {
	path := recordUnder(w.PID, filepath.Join(home, ".claude", "projects"))
	if path == "" {
		return Facts{}, false
	}
	return claudeFactsFrom(path)
}

// claudeFactsFrom reads one transcript. Split out from the search so the parsing can be tested
// against a real file without a live process to hand.
func claudeFactsFrom(path string) (Facts, bool) {
	facts := Facts{Harness: "claude", SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}

	var model string
	var last claudeUsage
	var found bool
	for _, line := range strings.Split(string(tailWithPayload(path, "message")), "\n") {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Message struct {
				Model string      `json:"model"`
				Usage claudeUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Message.Model != "" {
			model = event.Message.Model
		}
		if event.Message.Usage.InputTokens > 0 || event.Message.Usage.OutputTokens > 0 {
			last, found = event.Message.Usage, true
		}
	}
	if !found && model == "" {
		return Facts{}, false
	}
	facts.Model = model
	// Claude records the window per message as the harness's own view of what the request
	// carried: input plus both kinds of cache. Its own status line is a percentage of the
	// model's window, which the transcript does not name — so the panel shows tokens here and
	// leaves the percentage to a harness that states the window.
	facts.ContextUsed = last.InputTokens + last.CacheReadInputTokens + last.CacheCreationInputTokens
	facts.TotalTokens = facts.ContextUsed + last.OutputTokens
	facts.CachedTokens = last.CacheReadInputTokens
	return facts, true
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

// ---------------------------------------------------------------- reading

// fileWindow is a slice read out of a record file, with enough of its shape remembered to tell a
// whole line from half of one at its edges.
//
// The distinction is not decoration. A half-line is a JSON parse error, and a parse error here
// reads exactly like "this harness recorded nothing" — while dropping an edge that is whole reads
// exactly like "this harness recorded something else". Only the window knows which of its two
// edges are cut: one that begins at byte zero has a whole first line, and one that reaches the last
// byte has a whole last line.
type fileWindow struct {
	body      []byte
	startsAt0 bool
	endsAtEOF bool
}

// rawHead reads the beginning of a file.
func rawHead(path string, limit int64) fileWindow {
	file, err := os.Open(path)
	if err != nil {
		return fileWindow{}
	}
	defer func() { _ = file.Close() }()
	buf := make([]byte, limit+1)
	n, _ := io.ReadFull(file, buf)
	if int64(n) <= limit {
		// The file is shorter than the window, so this read is the whole of it.
		return fileWindow{body: buf[:n], startsAt0: true, endsAtEOF: true}
	}
	return fileWindow{body: buf[:limit], startsAt0: true}
}

// rawTail reads the end of a file, which is where a growing record keeps its newest news.
func rawTail(path string, limit int64) fileWindow {
	file, err := os.Open(path)
	if err != nil {
		return fileWindow{}
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fileWindow{}
	}
	start := info.Size() - limit
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return fileWindow{}
	}
	buf := make([]byte, info.Size()-start)
	n, _ := io.ReadFull(file, buf)
	return fileWindow{body: buf[:n], startsAt0: start == 0, endsAtEOF: true}
}

// lastJSONFromHead returns the payload of the last whole line whose type is one of kinds, from a
// window that was read from the beginning of a file: the head is where codex states the turn it is
// running in.
func lastJSONFromHead(w fileWindow, kinds ...string) json.RawMessage {
	var found json.RawMessage
	for _, line := range w.lines() {
		if payload := payloadOf(line, kinds); payload != nil {
			found = payload
		}
	}
	return found
}

func lastJSONFromTail(path string, kinds ...string) json.RawMessage {
	for _, window := range []int64{256 << 10, 1 << 20, 4 << 20, 16 << 20} {
		w := rawTail(path, window)
		if len(w.body) == 0 {
			return nil
		}
		var found json.RawMessage
		for _, line := range w.lines() {
			if payload := payloadOf(line, kinds); payload != nil {
				found = payload
			}
		}
		if found != nil {
			return found
		}
	}
	return nil
}

// tailWithPayload is lastJSONFromTail for claude, whose records are not wrapped in a type
// field: they are messages, and the newest one carries the newest numbers.
func tailWithPayload(path string, _ string) []byte {
	for _, window := range []int64{256 << 10, 1 << 20, 4 << 20} {
		w := rawTail(path, window)
		if strings.Contains(string(w.body), "\"usage\"") || len(w.body) == 0 {
			return w.body
		}
	}
	return rawTail(path, 4<<20).body
}

// lines splits a window into the lines that are whole inside it, dropping only the edges it cut.
func (w fileWindow) lines() [][]byte {
	lines := strings.Split(string(w.body), "\n")
	if !w.endsAtEOF && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	if !w.startsAt0 && len(lines) > 0 {
		lines = lines[1:]
	}
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			out = append(out, []byte(line))
		}
	}
	return out
}

// payloadOf pulls the payload out of one record line, when its type is wanted.
func payloadOf(line []byte, kinds []string) json.RawMessage {
	if len(line) == 0 || line[0] != '{' {
		return nil
	}
	var record struct {
		Type string          `json:"type"`
		Raw  json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(line, &record) != nil || record.Raw == nil {
		return nil
	}
	// A record's type is not the only place a kind appears: codex wraps most things in a
	// generic `event_msg`, and says what it really is *inside* the payload (`"type":
	// "token_count"`). Matching only the outer name is how the first version of this read
	// nothing at all from either generation of the format.
	var inner struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(record.Raw, &inner)
	for _, kind := range kinds {
		if record.Type == kind || inner.Type == kind {
			return record.Raw
		}
	}
	return nil
}

// lastContextWindow finds the newest statement of the model's window size.
func lastContextWindow(path string) int64 {
	raw := lastJSONFromTail(path, "token_count")
	if raw == nil {
		return 0
	}
	var payload struct {
		ModelContextWindow int64      `json:"model_context_window"`
		Info               *codexInfo `json:"info"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return 0
	}
	if payload.ModelContextWindow > 0 {
		return payload.ModelContextWindow
	}
	if payload.Info != nil {
		return payload.Info.ModelContextWindow
	}
	return 0
}

// isEmpty reports whether nothing was read. Used by the tests to pin the difference between an
// empty answer and an unreadable one.
func (f Facts) isEmpty() bool {
	return f.Model == "" && f.ContextUsed == 0 && f.TotalTokens == 0 && f.Goal == "" && f.Access == ""
}
