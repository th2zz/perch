package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/th2zz/perch/bridge/internal/herdr"
)

// ErrNotBlocked guards against acting on an agent that is no longer waiting.
//
// This is a safety property, not a UX nicety: sending Enter or Esc into an
// agent that already resumed can inject a keystroke into unrelated work. The
// queue treats it as "handled elsewhere" (spec US2 scenario 3), and the mapper
// treats it as a refusal.
var ErrNotBlocked = errors.New("target is not waiting for a decision")

// UndeterminableError carries the human-readable reason a mapping was refused.
// It wraps ErrNoDeterministicKeys so callers can test with errors.Is while
// still surfacing the specific explanation to the user.
type UndeterminableError struct{ Reason string }

func (e *UndeterminableError) Error() string {
	return ErrNoDeterministicKeys.Error() + ": " + e.Reason
}

func (e *UndeterminableError) Is(target error) bool { return target == ErrNoDeterministicKeys }

// Mapper maps decisions to herdr commands. It is stateless.
type Mapper struct{}

// MethodPaneAnswer is this product's own name for "write into the pane and submit it": there is no
// `pane.answer` in herdr, and the two calls it takes are not one call upstream. It is spelled here,
// next to the mapper, precisely because it is *ours* — the names in the herdr package are hers.
const MethodPaneAnswer = "pane.answer"

// NewMapper returns the default mapper.
func NewMapper() *Mapper { return &Mapper{} }

// Map resolves one decision against what is known about the target.
//
// The returned Command is the single herdr operation to perform. Any error
// means nothing must be sent.
func (m *Mapper) Map(req Request, obs Observation) (Command, error) {
	if err := req.Validate(); err != nil {
		return Command{}, err
	}

	switch req.Kind {
	case KindApprove, KindReject:
		return m.mapKeystroke(req, obs)
	case KindAnswer:
		if !obs.RecognisedAgent {
			return Command{}, fmt.Errorf("%w: cannot answer a question in %s", ErrNoAgent, req.Target)
		}
		return m.mapAnswer(req, obs)
	case KindPrompt:
		if !obs.RecognisedAgent || obs.AgentStatus == herdr.StatusBlocked {
			// Escape-hatch variant of the same write class (A5): the user wrote
			// text, we deliver it to the pane they are looking at. This is not a
			// fifth class of write.
			//
			// A blocked agent is included here because herdr refuses
			// `agent.prompt` while a dialog is up: the only way to deliver text
			// to a live dialog is to write into the pane.
			return Command{
				Kind:   KindPrompt,
				Method: herdr.MethodPaneSendText,
				Target: req.Target,
				Text:   req.Text,
				CLI:    herdr.WithProgram(herdr.SendTextCommand(req.Target, req.Text)),
				Remote: herdr.RemoteArgs(herdr.WithProgram(herdr.SendTextCommand(req.Target, req.Text))),
			}, nil
		}
		if herdr.StartsInPane(obs.AgentKind) {
			cli := herdr.RunCommand(req.Target, req.Text)
			return Command{
				Kind:   KindPrompt,
				Method: MethodPaneAnswer,
				Target: req.Target,
				Text:   req.Text,
				Keys:   []string{"enter"},
				CLI:    herdr.WithProgram(cli),
				Remote: herdr.RemoteArgs(cli),
			}, nil
		}
		return m.mapPrompt(req, obs, herdr.MethodAgentPrompt)
	default:
		return Command{}, fmt.Errorf("%w: %q", ErrUnknownKind, req.Kind)
	}
}

// mapAnswer answers a question, using whatever herdr actually permits.
//
// The distinction that matters: `agent.prompt` submits a *prompt* and is refused
// with `agent_blocked` while a dialog is on screen, so a blocked agent can only be
// answered by writing into the pane and then submitting. herdr's documentation
// says exactly this ("inspect the dialog and use agent send-keys for a deliberate
// response"), and getting it wrong is not subtle: the button would fail every time
// on precisely the dialogs it exists for.
//
// The submission key is only used where the manifest says the dialog expects
// typing. Everywhere else, writing text into a menu would be garbage typed into a
// menu, so we refuse and point at the escape hatch instead.
func (m *Mapper) mapAnswer(req Request, obs Observation) (Command, error) {
	if obs.AgentStatus != herdr.StatusBlocked {
		// The agent is ready for input, so this is an ordinary prompt.
		return m.mapPrompt(req, obs, herdr.MethodAgentPrompt)
	}

	keys, reason, found := LookupKeys(obs.AgentKind, obs.Rule)
	if !found || !keys.AcceptsTypedAnswer() {
		if reason == "" {
			reason = "this dialog is not one Perch can type into; open the session on your computer"
		}
		return Command{}, &UndeterminableError{Reason: reason}
	}

	// One request that writes the text and presses the submit key, rather than two
	// that happen to be adjacent: herdr documents the combined form as the one that
	// honours bracketed-paste mode, which is what keeps the newlines inside a
	// multi-line answer from being read as several separate submissions.
	cli := herdr.RunCommand(req.Target, req.Text)
	if len(keys.Answer) != 1 || keys.Answer[0] != "enter" {
		// `pane run` always submits with Enter. A dialog with a different submit key
		// is the low-level case, and the equivalent command says so plainly.
		cli = herdr.Then(
			herdr.SendTextCommand(req.Target, req.Text),
			herdr.SendKeysCommand(req.Target, keys.Answer),
		)
	}
	return Command{
		Kind:   KindAnswer,
		Method: MethodPaneAnswer,
		Target: req.Target,
		Text:   req.Text,
		Keys:   keys.Answer,
		CLI:    herdr.WithProgram(cli),
		// Two steps only in the low-level case: a dialog whose submit key is not
		// Enter cannot be answered by `pane run`, which always presses Enter.
		Remote: herdr.RemoteArgs(cli),
	}, nil
}

func (m *Mapper) mapKeystroke(req Request, obs Observation) (Command, error) {
	if !obs.RecognisedAgent {
		return Command{}, fmt.Errorf("%w: cannot %s in %s", ErrNoAgent, req.Kind, req.Target)
	}
	if obs.AgentStatus != herdr.StatusBlocked {
		return Command{}, fmt.Errorf("%w: %s is %s", ErrNotBlocked, req.Target, obs.AgentStatus)
	}

	keys, reason, ok := LookupKeysForPrompt(obs.AgentKind, obs.Rule, obs.PromptText)
	if !ok {
		return Command{}, &UndeterminableError{Reason: reason}
	}

	var seq []string
	switch req.Kind {
	case KindApprove:
		seq = keys.Approve
	case KindReject:
		seq = keys.Reject
	}
	if len(seq) == 0 {
		detail := reason
		if detail == "" {
			detail = "rule " + obs.Rule + " has no " + string(req.Kind) + " key"
		}
		return Command{}, &UndeterminableError{Reason: detail}
	}

	// Most agents are addressable by name through `agent.send_keys`. dsh-tui is
	// started by typing its launcher into a pane, so herdr detects it but has no
	// named agent to address; `agent.send_keys` answers "not an active named
	// agent". The pane-scoped write is the same deliberate key press and is the
	// documented path for a terminal actor.
	if herdr.StartsInPane(obs.AgentKind) {
		cli := herdr.SendKeysCommand(req.Target, seq)
		return Command{
			Kind:   req.Kind,
			Method: herdr.MethodPaneSendKeys,
			Target: req.Target,
			Keys:   seq,
			CLI:    herdr.WithProgram(cli),
			Remote: herdr.RemoteArgs(cli),
		}, nil
	}
	cli := herdr.AgentSendKeysCommand(req.Target, seq)
	return Command{
		Kind:   req.Kind,
		Method: herdr.MethodAgentSendKeys,
		Target: req.Target,
		Keys:   seq,
		CLI:    herdr.WithProgram(cli),
		Remote: herdr.RemoteArgs(cli),
	}, nil
}

// mapPrompt submits text to an agent that is ready for it.
//
// Only ever reached when the agent is not blocked: herdr refuses `agent.prompt`
// outright while a dialog is up, and Map routes that case to the pane write
// instead. Keeping the blocked case out of here means there is exactly one place
// that knows about the restriction.
func (m *Mapper) mapPrompt(req Request, obs Observation, method string) (Command, error) {
	cli := herdr.AgentPromptCommand(req.Target, req.Text)
	return Command{
		Kind:   req.Kind,
		Method: method,
		Target: req.Target,
		Text:   req.Text,
		CLI:    herdr.WithProgram(cli),
		Remote: herdr.RemoteArgs(cli),
	}, nil
}

// Execute performs the command through the socket client. The CLI form is
// recorded on the Command for audit and tests; the socket call is what runs.
func (m *Mapper) Execute(ctx context.Context, c *herdr.Client, cmd Command) error {
	switch cmd.Method {
	case herdr.MethodAgentSendKeys:
		return c.SendKeys(ctx, cmd.Target, cmd.Keys)
	case herdr.MethodAgentPrompt:
		return c.Prompt(ctx, cmd.Target, cmd.Text)
	case herdr.MethodPaneSendText:
		return c.SendText(ctx, cmd.Target, cmd.Text)
	case herdr.MethodPaneSendKeys:
		return c.SendKeysToPane(ctx, cmd.Target, cmd.Keys)
	case MethodPaneAnswer:
		return c.SendInput(ctx, cmd.Target, cmd.Text, cmd.Keys)
	default:
		return fmt.Errorf("refusing to execute unknown method %q", cmd.Method)
	}
}

// ExecuteRemote performs the command on another machine.
//
// It runs the command line the app already shows the user, against a saved SSH
// machine — so on a remote host "what the button does" and "what the screen says
// it does" are not merely equivalent, they are the same thing (constitution II).
// The local path keeps using the socket client, which is faster and is the path
// every existing verification covers.
func (m *Mapper) ExecuteRemote(ctx context.Context, cli *herdr.CLI, machine string, cmd Command) error {
	if len(cmd.Remote) == 0 {
		// Refusing is the only safe answer: the alternative is to guess at a
		// command for a machine we cannot see.
		return fmt.Errorf("refusing to run %q on %s: no equivalent command was built for it", cmd.Method, machine)
	}
	for _, argv := range cmd.Remote {
		if err := cli.RunMachine(ctx, machine, argv...); err != nil {
			return err
		}
	}
	return nil
}
