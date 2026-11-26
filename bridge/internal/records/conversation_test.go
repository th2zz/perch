package records

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These lines are copied from this machine's own codex rollout
// (`~/.codex/sessions/2026/10/11/rollout-…-01a128ff-…jsonl`) with the long text values cut short.
// Nothing about the *shape* is invented, and the shapes here are the ones that decide whether the
// reading face is right:
//
//   - the same answer arrives **twice** — once as an `event_msg` item and once as a `response_item`
//     with content type `output_text` — so a reader that takes both would show every answer twice;
//   - the system's own injections, including the environment context, arrive as `response_item`
//     `role: "user"`, so a reader that takes every user-ish line would show the user a wall of
//     prompts they never wrote;
//   - codex labels its own messages with a `phase` rather than by position, which is what makes
//     "the final answer" a field to read instead of a guess to make.
const (
	lineCodexSessionMeta = `{"timestamp":"2026-10-11T03:28:03.435Z","ordinal":0,"type":"session_meta","payload":{"session_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","cwd":"/home/user/workspace","originator":"codex-tui","cli_version":"0.162.1","source":"vscode"}}`
	lineCodexWorldState  = `{"timestamp":"2026-10-11T03:28:03.479Z","ordinal":6,"type":"world_state","payload":{"full":true,"state":{"collaboration_mode":{"mode":"default","model":"deepseek-flash"}}}}`
	lineCodexTaskStarted = `{"timestamp":"2026-10-14T05:44:11.378Z","ordinal":26377,"type":"event_msg","payload":{"type":"task_started","turn_attribution":{"turn_id":"01a138f0-8cfc-7c82-b939-9cd1e5d1f054","turn_trigger":"user","parent_turn_id":null,"initiating_agent_path":null,"root_turn_id":"01a138f0-8cfc-7c82-b939-9cd1e5d1f054"},"turn_id":"01a138f0-8cfc-7c82-b939-9cd1e5d1f054","root_turn_id":"01a138f0-8cfc-7c82-b939-9cd1e5d1f054","started_at":1791956651,"model_context_window":996147,"collaboration_mode_kind":"default"}}`
	lineCodexTaskDone    = `{"timestamp":"2026-10-14T04:24:17.590Z","ordinal":26375,"type":"event_msg","payload":{"type":"task_complete","root_turn_id":"01a1389a-c7fa-7ac1-a403-eac55772ad92","turn_id":"01a1389a-c7fa-7ac1-a403-eac55772ad92","last_agent_message":"Answering all three points.","started_at":1791951030,"completed_at":1791951857,"duration_ms":827315,"time_to_first_token_ms":6760}}`

	// The environment context: a `response_item` that claims to be from the user and is not.
	lineCodexEnvironment = `{"timestamp":"2026-10-11T03:28:03.475Z","ordinal":5,"type":"response_item","payload":{"type":"message","id":"msg_01a12900-d792-7371-a523-fc599b598e89","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/home/user/workspace</cwd>\n  <shell>bash</shell>\n</environment_context>"}]}}`
	lineCodexDeveloper   = `{"timestamp":"2026-10-11T03:28:03.474Z","ordinal":2,"type":"response_item","payload":{"type":"message","id":"msg_01a12900-d792-7371-a523-fc2efb2256a0","role":"developer","content":[{"type":"input_text","text":"<skills_instructions>\n## Skills\nA skill is a set of local instructions…"}]}}`

	lineCodexUserMessage = `{"timestamp":"2026-10-14T05:44:11.454Z","ordinal":26380,"type":"event_msg","payload":{"type":"item_completed","thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","turn_id":"01a138f0-8cfc-7c82-b939-9cd1e5d1f054","item":{"type":"UserMessage","id":"01a138f0-8dbc-7531-90e1-928fcc1e41c2","client_id":"215955ed-f1aa-4d51-8b8e-ea0447f562e4","content":[{"type":"text","text":"Can you verify why syntax highlighting is not visible in the session view?"}]},"started_at_ms":1791956651453,"completed_at_ms":1791956651454}}`
	lineCodexAgentsMd    = `{"timestamp":"2026-10-16T16:24:25.675Z","ordinal":5,"type":"response_item","payload":{"type":"message","id":"msg_01a14587-6d4a-7e93-b39b-ab056c73a019","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions\n\n<INSTRUCTIONS>\n# Guidelines\n\n## 0. AGENTS.md maintenance\nDo not edit this section.\n1. Keep AGENTS.md under 400 words / ~500 tokens.\n…\n</INSTRUCTIONS>"}]}}`
	lineCodexReasoning   = `{"timestamp":"2026-10-14T04:21:40.161Z","ordinal":26342,"type":"event_msg","payload":{"type":"item_completed","thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","turn_id":"01a1389a-c7fa-7ac1-a403-eac55772ad92","item":{"type":"Reasoning","id":"abae12fe-b0f0-47da-9eb8-fb31d96dec8a","summary_text":[],"raw_content":["Now it looks right:\n- The Kotlin block: @Composable (yellow-ish)"]},"started_at_ms":1791951698524,"completed_at_ms":1791951700161}}`
	lineCodexCommand     = `{"timestamp":"2026-10-14T04:22:39.401Z","ordinal":26350,"type":"event_msg","payload":{"type":"item_completed","thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","turn_id":"01a1389a-c7fa-7ac1-a403-eac55772ad92","item":{"type":"CommandExecution","id":"call_00_DfH9MXsio0utDNfjorS44765","process_id":"14167","command":["/bin/bash","-lc","cd /home/user/workspace/perch && export PATH=$HOME/.local/opt/go/bin:$PATH"],"cwd":"file:///home/user/workspace","status":"completed","aggregated_output":"GO_OK\nBUILD SUCCESSFUL in 56s\n","exit_code":0,"duration":{"secs":58,"nanos":622229330}},"started_at_ms":1791951700777,"completed_at_ms":1791951759400}}`
	lineCodexCommentary  = `{"timestamp":"2026-10-14T05:46:51.815Z","ordinal":26402,"type":"event_msg","payload":{"type":"item_completed","thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","turn_id":"01a138f2-b072-7ec3-ab5e-c451fc5bc764","item":{"type":"AgentMessage","id":"aafb9c16-8119-4a4d-8b5c-1252fd5ce0cb","content":[{"type":"Text","text":"Checking the parser for box-drawn tables."}],"phase":"commentary"},"started_at_ms":1791956811013,"completed_at_ms":1791956811815}}`
	lineCodexTokenUsage  = `{"timestamp":"2026-10-14T04:21:40.614Z","ordinal":26345,"type":"token_usage_record","payload":{"thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","usage":{"input_tokens":738675,"cached_input_tokens":737664,"output_tokens":442,"total_tokens":739117},"thread_token_usage":{"input_tokens":1602555119,"cached_input_tokens":1598308087,"output_tokens":2903688,"total_tokens":1605458807}}}`
	lineCodexTokenCount  = `{"timestamp":"2026-10-14T04:21:29.849Z","ordinal":26341,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1601816444,"output_tokens":2903246,"total_tokens":1604719690},"last_token_usage":{"input_tokens":737696,"output_tokens":62,"total_tokens":737758},"model_context_window":996147}}}`

	// The answer, as the TUI's own record carries it…
	lineCodexFinalAnswer = `{"timestamp":"2026-10-14T04:24:17.500Z","ordinal":26371,"type":"event_msg","payload":{"type":"item_completed","thread_id":"01a128ff-4c2c-7a20-b253-66798d3a28ae","turn_id":"01a1389a-c7fa-7ac1-a403-eac55772ad92","item":{"type":"AgentMessage","id":"a0cf01e1-15f1-40e3-9b5d-e15e338166b7","content":[{"type":"Text","text":"Fixed the root cause: **the pane layout is restored** (58 rows, composer and status line intact)."}],"phase":"final_answer"},"started_at_ms":1791951851319,"completed_at_ms":1791951857500}}`
	// …and the same answer again, as the model saw it. Both are real; taking both shows it twice.
	lineCodexAnswerAsResponseItem = `{"timestamp":"2026-10-14T04:24:17.536Z","ordinal":26372,"type":"response_item","payload":{"type":"message","id":"a0cf01e1-15f1-40e3-9b5d-e15e338166b7","role":"assistant","content":[{"type":"output_text","text":"Fixed the root cause: **the pane layout is restored** (58 rows, composer and status line intact)."}],"phase":"final_answer"}}`
)

const (
	lineCodexLongQuestionOnly = `{"timestamp":"2026-10-15T06:19:02.920Z","ordinal":34477,"type":"response_item","payload":{"type":"message","id":"msg_01a13e36-d388-7ac2-a23c-b26dc72585bb","role":"user","content":[{"type":"input_text","text":"Reconnected now. Can we optimize the reconnect latency and keep pinch zoom working after reopening the session screen?"}]}}`
	lineCodexAnswerOnly       = `{"timestamp":"2026-10-15T06:41:07.100Z","ordinal":34510,"type":"response_item","payload":{"type":"message","id":"msg_01a13e37-1111-2222-3333-444455556666","role":"assistant","content":[{"type":"output_text","text":"Two things: tighten the idle pool window and retry once on a fresh socket."}],"phase":"final_answer"}}`
	lineCodexCommentaryOnly   = `{"timestamp":"2026-10-15T06:41:00.000Z","ordinal":34509,"type":"response_item","payload":{"type":"message","id":"msg_01a13e37-aaaa-bbbb-cccc-ddddeeeeffff","role":"assistant","content":[{"type":"output_text","text":"Checking the logs before answering."}],"phase":"commentary"}}`
	lineCodexGoalCleared      = `{"timestamp":"2026-10-15T06:17:00.000Z","ordinal":34470,"type":"response_item","payload":{"type":"message","id":"msg_01a13e35-9999-9999-9999-999999999999","role":"user","content":[{"type":"input_text","text":"<codex_internal_context source=\"user_goal\">\nUser cleared the goal.\n</codex_internal_context>"}]}}`
	lineCodexGoalContext      = `{"timestamp":"2026-10-15T06:19:02.902Z","ordinal":34476,"type":"response_item","payload":{"type":"message","id":"msg_01a13e36-0000-0000-0000-000000000000","role":"user","content":[{"type":"input_text","text":"<codex_internal_context source=\"user_goal\">\nUser set the goal: Connect a standalone shell to the host\n</codex_internal_context>"}]}}`
)

func TestConversationKeepsAMessageThatOnlyExistsAsAResponseItem(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		lineCodexSessionMeta,
		lineCodexGoalContext,
		lineCodexLongQuestionOnly,
		lineCodexCommentaryOnly,
		lineCodexAnswerOnly,
	)

	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	want := []Message{
		// What really happened to that message, said plainly instead of being invisible: codex's goal
		// feature had been triggered, so the words went in as a goal and never as a message.
		{Role: "you", Text: "Set a goal: Connect a standalone shell to the host"},
		{Role: "you", Text: "Reconnected now. Can we optimize the reconnect latency and keep pinch zoom working after reopening the session screen?"},
		{Role: "agent", Text: "Two things: tighten the idle pool window and retry once on a fresh socket."},
	}
	if len(transcript.Messages) != len(want) {
		t.Fatalf("kept %d messages, want %d: %+v", len(transcript.Messages), len(want), transcript.Messages)
	}
	for i, message := range want {
		if transcript.Messages[i] != message {
			t.Errorf("message %d:\n got %+v\nwant %+v", i, transcript.Messages[i], message)
		}
	}
}

// A goal that was *cleared* says so too: the face is a record of what the person did, and a cleared goal is
// as much a part of that as a set one.
func TestConversationSaysWhenAGoalWasCleared(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexSessionMeta, lineCodexGoalCleared)

	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(transcript.Messages) != 1 || transcript.Messages[0] != (Message{Role: "you", Text: "Cleared the goal."}) {
		t.Fatalf("a cleared goal reads as %+v", transcript.Messages)
	}
}

func TestConversationSkipsCodexInjectedAgentsInstructions(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		lineCodexSessionMeta,
		lineCodexAgentsMd,
		lineCodexUserMessage,
		lineCodexFinalAnswer,
	)

	transcript, readable := Conversation("codex", home, Where{Argv: codexArgv()})
	if !readable {
		t.Fatal("codex is readable; the reader says otherwise")
	}
	if len(transcript.Messages) != 2 {
		t.Fatalf("kept %d messages, want 2: %+v", len(transcript.Messages), transcript.Messages)
	}
	if strings.HasPrefix(transcript.Messages[0].Text, "# AGENTS.md instructions") {
		t.Fatalf("the reading face shows codex's injected instructions as the person's message: %+v", transcript.Messages[0])
	}
}

// codexArgv is what the pane's process looks like when codex has been resumed: the session id is on
// the command line, which is how the reader knows *which* of a machine's codex sessions this is.
func codexArgv() []string {
	return []string{"node", "/home/user/.local/opt/node/bin/codex", "resume", codexSessionIDValue}
}

func TestConversationFindsTheRecordByTheSessionHerdrReported(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexSessionMeta, lineCodexUserMessage, lineCodexFinalAnswer)

	where := Where{
		PaneID: "wA:p1",
		Argv:   []string{"node", "/home/user/.local/opt/node/bin/codex"},
		// A real pid whose tree holds no record: the registration must still be taken.
		PID:       os.Getpid(),
		SessionID: codexSessionIDValue,
	}
	transcript, readable := Conversation("codex", home, where)
	if !readable {
		t.Fatal("herdr named the session; the reader still could not find its record")
	}
	if len(transcript.Messages) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(transcript.Messages), transcript.Messages)
	}
	if transcript.Messages[0].Role != RoleYou || transcript.Messages[1].Role != RoleAgent {
		t.Errorf("roles = %q then %q", transcript.Messages[0].Role, transcript.Messages[1].Role)
	}
}

func TestConversationKeepsQuestionsAndFinalAnswersOnly(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home,
		lineCodexSessionMeta,
		lineCodexWorldState,
		lineCodexTaskStarted,
		lineCodexDeveloper,
		lineCodexEnvironment,
		lineCodexUserMessage,
		lineCodexReasoning,
		lineCodexCommand,
		lineCodexCommentary,
		lineCodexTokenUsage,
		lineCodexTokenCount,
		lineCodexFinalAnswer,
		lineCodexAnswerAsResponseItem,
		lineCodexTaskDone,
	)

	transcript, readable := Conversation("codex", home, Where{Argv: codexArgv()})
	if !readable {
		t.Fatal("codex is readable; the reader says otherwise")
	}
	want := []Message{
		{Role: "you", Text: "Can you verify why syntax highlighting is not visible in the session view?"},
		{Role: "agent", Text: "Fixed the root cause: **the pane layout is restored** (58 rows, composer and status line intact)."},
	}
	if len(transcript.Messages) != len(want) {
		t.Fatalf("kept %d messages, want %d: %+v", len(transcript.Messages), len(want), transcript.Messages)
	}
	for i, message := range want {
		if transcript.Messages[i] != message {
			t.Errorf("message %d:\n got %+v\nwant %+v", i, transcript.Messages[i], message)
		}
	}
	if transcript.Truncated {
		t.Error("this record was read whole; the transcript says it starts part-way through")
	}
}

func TestConversationSaysWhenATurnIsInFlight(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexUserMessage, lineCodexTaskStarted)

	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if transcript.WorkingSince.IsZero() {
		t.Fatal("a turn that has started and not finished is work in progress")
	}
	if got, want := transcript.WorkingSince.Unix(), int64(1791956651); got != want {
		t.Errorf("working since %d, want %d", got, want)
	}

	// A finished turn is not. This is the difference between a phone that can say "working ·
	// 2m" and one that keeps saying it after the agent has gone quiet.
	writeCodexRecord(t, home, lineCodexUserMessage, lineCodexTaskStarted, lineCodexTaskDone)
	transcript, _ = Conversation("codex", home, Where{Argv: codexArgv()})
	if !transcript.WorkingSince.IsZero() {
		t.Errorf("a finished turn still looks like work in progress: %s", transcript.WorkingSince)
	}
}

func TestConversationFollowsTheFileWithoutReReadingIt(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexUserMessage)

	first, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(first.Messages) != 1 {
		t.Fatalf("first read kept %d messages, want 1", len(first.Messages))
	}

	// The write is in flight: a line that has arrived without its newline yet must be held, not
	// parsed half-way and not dropped. This is the ordinary case, not a rare one — the app polls
	// every 0.8 seconds while an agent is answering.
	half := lineCodexFinalAnswer[:len(lineCodexFinalAnswer)/2]
	appendCodexRaw(t, home, half)
	mid, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(mid.Messages) != 1 {
		t.Fatalf("a half-written line was read as a message: %+v", mid.Messages)
	}

	appendCodexRaw(t, home, lineCodexFinalAnswer[len(lineCodexFinalAnswer)/2:]+"\n")
	last, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(last.Messages) != 2 {
		t.Fatalf("after the line completed: %d messages, want 2: %+v", len(last.Messages), last.Messages)
	}
	if last.Messages[1].Role != "agent" {
		t.Errorf("the completed line arrived as %q", last.Messages[1].Role)
	}

	// Read against a file that has not changed: the same conversation, once, not twice.
	again, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(again.Messages) != 2 {
		t.Errorf("a re-read of an unchanged file gave %d messages, want 2", len(again.Messages))
	}
}

func TestConversationReadsOnlyTheTailOfALargeRecord(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexUserMessage, lineCodexFinalAnswer)
	for i := 0; i < (firstReadWindow/len(lineCodexTokenCount))+16; i++ {
		appendCodexRecord(t, home, lineCodexTokenCount)
	}
	appendCodexRecord(t, home, lineCodexUserMessage)
	appendCodexRecord(t, home, lineCodexTaskStarted)

	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(transcript.Messages) != 1 {
		t.Fatalf("kept %d messages; the window should have started past the older ones", len(transcript.Messages))
	}
	if transcript.Messages[0].Role != "you" {
		t.Errorf("kept %+v", transcript.Messages[0])
	}
	if transcript.WorkingSince.IsZero() {
		t.Error("the turn start inside the window was not read")
	}
	// And the reading face has to know it is not looking at the beginning: the window started part
	// way through this file, so there are messages before these that are not being sent.
	if !transcript.Truncated {
		t.Error("a record read from a window part-way through reports itself as complete")
	}
}

func TestConversationKeepsTheNewestWhenThereAreTooMany(t *testing.T) {
	home := t.TempDir()
	// Each line is numbered, so "kept the newest" is a claim a test can check rather than a claim
	// about a count. A phone that keeps the *oldest* eighty is a phone showing four-day-old answers
	// with the newest one missing.
	line := func(i int) string {
		return strings.Replace(lineCodexUserMessage, "Can you verify why syntax highlighting is not visible in the session view?", fmt.Sprintf("Message %d", i), 1)
	}
	const total = maxMessages + 20
	writeCodexRecord(t, home, line(0))
	for i := 1; i < total; i++ {
		appendCodexRecord(t, home, line(i))
	}

	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(transcript.Messages) != maxMessages {
		t.Fatalf("kept %d messages, want %d", len(transcript.Messages), maxMessages)
	}
	if want := fmt.Sprintf("Message %d", total-maxMessages); !strings.Contains(transcript.Messages[0].Text, want) {
		t.Errorf("oldest kept message is %q, want %q", transcript.Messages[0].Text, want)
	}
	if want := fmt.Sprintf("Message %d", total-1); !strings.Contains(transcript.Messages[maxMessages-1].Text, want) {
		t.Errorf("newest kept message is %q, want %q", transcript.Messages[maxMessages-1].Text, want)
	}
	// Dropping the oldest twenty is a truncation, and the reader is told: a screen that silently
	// starts in the middle of a conversation is a screen that lies about where the beginning is.
	if !transcript.Truncated {
		t.Error("dropped the oldest messages and did not say so")
	}
}

func TestConversationStartsAgainWhenTheRecordIsReplaced(t *testing.T) {
	home := t.TempDir()
	writeCodexRecord(t, home, lineCodexUserMessage)
	if transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()}); len(transcript.Messages) != 1 {
		t.Fatalf("setup: got %d messages", len(transcript.Messages))
	}

	// A new session in the same file — codex does this, and so does a compaction. Keeping the old
	// conversation would show the user a transcript stitched from two sessions.
	writeCodexRecord(t, home, lineCodexFinalAnswer)
	transcript, _ := Conversation("codex", home, Where{Argv: codexArgv()})
	if len(transcript.Messages) != 1 {
		t.Fatalf("got %d messages, want the replacement's 1: %+v", len(transcript.Messages), transcript.Messages)
	}
	if transcript.Messages[0].Role != "agent" {
		t.Errorf("the message from before the replacement survived: %+v", transcript.Messages[0])
	}
}

func TestConversationSaysSoWhenItCannotReadTheHarness(t *testing.T) {
	home := t.TempDir()
	transcript, readable := Conversation("antigravity", home, Where{})
	if readable {
		t.Error("nothing has taught the reader antigravity's records; it says it can read them")
	}
	if len(transcript.Messages) != 0 {
		t.Errorf("an unreadable harness produced %+v", transcript.Messages)
	}
	// And an empty conversation is still empty rather than nil: the app has one shape to read.
	if transcript.Messages != nil {
		t.Errorf("messages should stay nil so the caller can substitute an empty list")
	}
}

// appendCodexRecord adds one complete line to the newest record, the way the harness does.
func appendCodexRecord(t *testing.T, home, line string) {
	t.Helper()
	appendCodexRaw(t, home, line+"\n")
}

// appendCodexRaw adds bytes with no line ending, for the half-written case: a record is a file being
// appended to by another process, so a read can land in the middle of a line. (Measured on this
// machine: codex's record always ends in a newline once a write has finished, which is why a
// complete last line is never held back.)
func appendCodexRaw(t *testing.T, home, raw string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no record to append to: %v", err)
	}
	file, err := os.OpenFile(matches[len(matches)-1], os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open record: %v", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(raw); err != nil {
		t.Fatalf("append: %v", err)
	}
}

// A sanity check on the fixture itself: the timestamps are seconds, which is what WorkingSince is
// built from. If a future codex writes milliseconds this fails here rather than in a user's screen.
func TestConversationFixtureUsesSeconds(t *testing.T) {
	at := time.Unix(1791956651, 0)
	if at.Year() != 2026 {
		t.Fatalf("the fixture's own timestamp is not a plausible time: %s", at)
	}
}
