package records

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

type Message struct {
	// Role is [RoleYou] or [RoleAgent]: whose words these are.
	Role string `json:"role"`
	Text string `json:"text"`
}

// The two roles, as a closed set on the wire.
//
// Named constants because the app compares these strings, and a rename would not raise anything on
// either side — it would quietly make every message look like the agent's. They are pinned against
// the shared vocabulary file by the app's own contract test
// (`contracts/wire-vocabulary.json`).
const (
	// RoleYou is a turn the user submitted.
	RoleYou = "you"
	// RoleAgent is an answer the agent decided to give.
	RoleAgent = "agent"
)

// Transcript is a session's conversation, as the harness recorded it.
type Transcript struct {
	Messages []Message `json:"messages"`
	// SessionID is the harness's own identity for the record that was read. It is
	// not on the wire; the bridge persists it as the pane's session identity.
	SessionID string `json:"-"`
	// WorkingSince is when the current turn started, or the zero time when the agent is idle.
	WorkingSince time.Time `json:"-"`
	// Truncated is true when older messages exist that this transcript does not carry — the record
	// was longer than the first-read window, or the conversation has outgrown [maxMessages].
	//
	// It travels because the reading face is the only place that can say it, and the difference
	// between "this is the whole conversation" and "this starts in the middle of one" is the
	// difference between reading and misreading.
	Truncated bool `json:"-"`
}

// Conversation reads the conversation for a pane's session.
//
// The second return says whether there is a conversation to show at all — the record was found and
// this harness's records are understood. It is false when an agent's records are unknown *and* when
// the record of a known agent could not be located, because the caller does the same good thing in
// both cases: it falls back to the screen. (The usage panel draws a different line: "nothing
// recorded yet" is a fine answer for a panel of numbers, and a blank reading area is not.)
func Conversation(harness, home string, w Where) (Transcript, bool) {
	switch harness {
	case "codex":
		path, id := codexRollout(home, w)
		if path == "" {
			return Transcript{}, false
		}
		transcript := codexConversation(path)
		transcript.SessionID = id
		return transcript, true
	default:
		return Transcript{}, false
	}
}

// ---------------------------------------------------------------- the tailing reader

// maxMessages bounds what a phone is asked to hold. A conversation longer than this is a conversation
// whose beginning belongs on the computer.
const maxMessages = 80

// firstReadWindow is how much of an existing record is parsed the first time.
//
// Not the whole file, and that matters: measured on this machine, four days of one session is 51 MB,
// and the phone's request path must not wait on parsing it. The window covers hundreds of exchanges,
// and the tail after it is read incrementally from then on.
const firstReadWindow = 8 << 20

// cursor is one file being followed: where the last read stopped, and what has been read so far.
type cursor struct {
	path    string
	offset  int64
	partial []byte
	lines   int64
	state   Transcript
	// What each side said last, so a message written twice (see responseItem) is kept once.
	lastYou   string
	lastAgent string
}

var (
	cursorMu sync.Mutex
	cursors  = map[string]*cursor{}
)

// codexConversation follows one rollout file, reading only what is new since the last call.
//
// It is a tail rather than a parse-everything because of the numbers above: the app re-reads a session
// every second and a half while an agent works, and a 51 MB file re-parsed at that rate would be the
// most expensive thing this product does.
func codexConversation(path string) Transcript {
	cursorMu.Lock()
	defer cursorMu.Unlock()

	c := cursors[path]
	if c == nil {
		c = &cursor{path: path}
		if info, err := os.Stat(path); err == nil && info.Size() > firstReadWindow {
			// Start where a line begins, so the first record parsed is a whole one.
			start := info.Size() - firstReadWindow
			if data, err := readAt(path, start, firstReadWindow); err == nil {
				if cut := strings.IndexByte(string(data), '\n'); cut >= 0 {
					c.offset = start + int64(cut) + 1
				} else {
					c.offset = start
				}
				// Something was skipped to get here, and the app says so.
				c.state.Truncated = true
			}
		}
		cursors[path] = c
	}
	if info, err := os.Stat(path); err == nil && info.Size() < c.offset {
		// The file was replaced or rewound (a new session, a compaction): start again rather than
		// showing a conversation stitched from two files.
		c.offset = 0
		c.partial = nil
		c.state = Transcript{}
	}

	if info, err := os.Stat(path); err == nil && info.Size() > c.offset {
		if data, err := readAt(path, c.offset, info.Size()-c.offset); err == nil {
			c.offset += int64(len(data))
			c.consume(data)
		}
	}
	return c.state
}

// consume parses whatever complete lines arrived, keeping any partial one for next time.
func (c *cursor) consume(data []byte) {
	buffer := append(c.partial, data...)
	lines := strings.Split(string(buffer), "\n")
	c.partial = []byte(lines[len(lines)-1])
	for _, line := range lines[:len(lines)-1] {
		c.line(strings.TrimSpace(line))
	}
}

// line reads one record.
//
// Only two kinds of thing are kept, and the shape of the record is what makes that exact rather than
// clever: codex wraps conversation items in `event_msg`s and *labels* an agent's own message with a
// phase, so "the final answer" is a field rather than a guess. `commentary` (the agent narrating what
// it is about to do), `Reasoning`, `CommandExecution` and `FileChange` are all skipped.
func (c *cursor) line(raw string) {
	if raw == "" || raw[0] != '{' {
		return
	}
	var record struct {
		Type    string `json:"type"`
		Payload struct {
			Type      string          `json:"type"`
			StartedAt int64           `json:"started_at"`
			Role      string          `json:"role"`
			Phase     string          `json:"phase"`
			Content   []contentBlock  `json:"content"`
			Item      json.RawMessage `json:"item"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(raw), &record) != nil {
		return
	}
	if record.Type == "response_item" {
		c.responseItem(record.Payload.Type, record.Payload.Role, record.Payload.Phase, record.Payload.Content)
		return
	}
	if record.Type != "event_msg" {
		return
	}
	switch record.Payload.Type {
	case "task_started":
		if record.Payload.StartedAt > 0 {
			c.state.WorkingSince = time.Unix(record.Payload.StartedAt, 0)
		}
	case "task_complete":
		c.state.WorkingSince = time.Time{}
	case "item_completed":
		c.item(record.Payload.Item)
	}
}

func (c *cursor) item(raw json.RawMessage) {
	if raw == nil {
		return
	}
	var item struct {
		Type    string         `json:"type"`
		Phase   string         `json:"phase"`
		Content []contentBlock `json:"content"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	var role string
	switch item.Type {
	case "UserMessage":
		role = RoleYou
	case "AgentMessage":
		// The whole point: only what the agent decided to say as its answer.
		if item.Phase != "final_answer" {
			return
		}
		role = RoleAgent
	default:
		return
	}
	c.appendText(role, joinText(item.Content))
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// joinText is what a message says, out of whatever blocks carry it.
//
// Codex writes "Text" in an agent's message and "text" in a user's, and `input_text`/`output_text` in the
// model-facing copies; all four are the same thing.
func joinText(blocks []contentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		switch strings.ToLower(block.Type) {
		case "text", "input_text", "output_text":
		default:
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(block.Text)
	}
	return text.String()
}

func (c *cursor) responseItem(kind, role, phase string, content []contentBlock) {
	if kind != "message" {
		return
	}
	switch role {
	case "user":
		body := strings.TrimSpace(joinText(content))
		if goal, ok := goalFromContext(body); ok {
			c.appendText(RoleYou, goal)
			return
		}
		if isInjectedContext(body) {
			return
		}
		c.appendText(RoleYou, body)
	case "assistant":
		// The phase is the whole reason this is allowed: commentary is the agent narrating, and only the
		// final answer is what the user asked for (D35).
		if phase != "final_answer" {
			return
		}
		c.appendText(RoleAgent, strings.TrimSpace(joinText(content)))
	}
}

func goalFromContext(body string) (string, bool) {
	if !strings.HasPrefix(body, "<codex_internal_context") ||
		!strings.Contains(body, `source="user_goal"`) {
		return "", false
	}
	inner := body
	if cut := strings.Index(inner, ">"); cut >= 0 {
		inner = inner[cut+1:]
	}
	inner = strings.TrimSuffix(strings.TrimSpace(inner), "</codex_internal_context>")
	inner = strings.TrimSpace(inner)
	switch {
	case strings.HasPrefix(inner, "User cleared the goal"):
		return "Cleared the goal.", true
	case strings.HasPrefix(inner, "User set the goal:"):
		objective := strings.TrimSpace(strings.TrimPrefix(inner, "User set the goal:"))
		if objective == "" || strings.HasPrefix(objective, "[objective omitted") {
			return "Set a goal (the text was too long for codex's own record — it is in the Usage panel).", true
		}
		return "Set a goal: " + objective, true
	default:
		return "", false
	}
}

// isInjectedContext says whether this "user" message was written by codex rather than by a person.
//
// The list is what this machine's own records show (measured 2026-10-15), not a guess: the wrappers codex
// injects are `<environment_context>`, `<user_instructions>`, `<skills_instructions>` and
// `<codex_internal_context>`; since 0.163 it also injects `# AGENTS.md instructions` + `<INSTRUCTIONS>`.
func isInjectedContext(body string) bool {
	firstLine := body
	if cut := strings.IndexByte(firstLine, '\n'); cut >= 0 {
		firstLine = firstLine[:cut]
	}
	if strings.TrimSpace(firstLine) == "# AGENTS.md instructions" &&
		strings.Contains(body, "<INSTRUCTIONS>") {
		return true
	}
	if !strings.HasPrefix(body, "<") {
		return false
	}
	// The *tag name*, not the whole opening string: codex's own goal wrapper carries an attribute
	// (`<codex_internal_context source="user_goal">`), which a prefix match on `<codex_internal_context>`
	// missed — the test caught it, which is the reason the fixture uses the real line rather than a tidy one.
	name := body[1:]
	if cut := strings.IndexAny(name, "> \t\r\n"); cut >= 0 {
		name = name[:cut]
	}
	switch name {
	case "environment_context", "user_instructions", "skills_instructions", "codex_internal_context",
		"turn_aborted", "system_instructions", "developer_instructions":
		return true
	}
	return false
}

// appendText keeps one message, enforces the cap, and drops an exact repeat from the same side.
//
// The repeat is not a mistake in the record: codex writes a message twice — once for its own interface and
// once for the model — and either shape may be the one that arrives. Being identical to the previous
// message *from the same side* is what identifies it, and it is the only safe rule: two identical messages
// from the user with an agent turn in between are two messages, not one.
func (c *cursor) appendText(role, raw string) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return
	}
	if (role == RoleYou && body == c.lastYou) || (role == RoleAgent && body == c.lastAgent) {
		return
	}
	if role == RoleYou {
		c.lastYou = body
	} else {
		c.lastAgent = body
	}
	c.lines++
	c.state.Messages = append(c.state.Messages, Message{Role: role, Text: body})
	if len(c.state.Messages) > maxMessages {
		c.state.Messages = c.state.Messages[len(c.state.Messages)-maxMessages:]
		c.state.Truncated = true
	}
}

// readAt reads a window of a file.
func readAt(path string, offset, length int64) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}
	buffer := make([]byte, length)
	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return nil, err
	}
	return buffer[:n], nil
}
