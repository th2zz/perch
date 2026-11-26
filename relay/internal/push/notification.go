package push

import (
	"fmt"
	"strings"
)

// Notification is the platform-agnostic message a device shows.
//
// The wording lives here rather than on the host so it can be translated and
// tuned without shipping a new bridge. It contains only what the summary
// carries: which computer, which project, which agent, and what is being asked.
type Notification struct {
	Title string
	Body  string
	// Data is what the app reads to deep-link straight to the request.
	Data map[string]string
}

// Render turns a summary into what the phone displays.
func Render(s Summary) Notification {
	agent := strings.TrimSpace(s.Agent)
	if agent == "" {
		agent = "An agent"
	}
	project := strings.TrimSpace(s.WorkspaceLabel)
	if project == "" {
		project = "a project"
	}

	title := fmt.Sprintf("%s needs you in %s", agent, project)
	body := "Tap to approve or answer."
	switch s.Kind {
	case "approve":
		body = "Waiting for your approval. Tap to allow."
	case "reject":
		body = "Waiting for your answer. Tap to decline."
	case "answer":
		body = "Asked you a question. Tap to reply."
	case "prompt":
		body = "Waiting for input. Tap to send an instruction."
	}
	if s.HostLabel != "" {
		body = fmt.Sprintf("%s (%s)", body, s.HostLabel)
	}

	return Notification{
		Title: title,
		Body:  body,
		Data: map[string]string{
			"decision_id": s.DecisionID,
			"host":        s.HostLabel,
			"kind":        s.Kind,
		},
	}
}
