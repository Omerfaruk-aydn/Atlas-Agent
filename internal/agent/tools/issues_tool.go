package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/issues"
)

//go:embed issues.md
var issuesDescription string

const IssuesToolName = "issues"

var issueLedgers sync.Map // session ID -> *issues.Ledger

// IssuesFor returns the session's open-issues ledger.
func IssuesFor(sessionID string) *issues.Ledger {
	if v, ok := issueLedgers.Load(sessionID); ok {
		return v.(*issues.Ledger)
	}
	v, _ := issueLedgers.LoadOrStore(sessionID, issues.New())
	return v.(*issues.Ledger)
}

type IssuesParams struct {
	Action   string `json:"action" description:"add, close or list."`
	Title    string `json:"title,omitempty" description:"For add: one line saying what is wrong."`
	Severity string `json:"severity,omitempty" description:"For add: minor, major or critical."`
	Where    string `json:"where,omitempty" description:"For add: the file and line, or the area, the problem is in."`
	ID       int    `json:"id,omitempty" description:"For close: the issue number."`
	Command  string `json:"command,omitempty" description:"For close: the command you ran to show it is fixed."`
	Output   string `json:"output,omitempty" description:"For close: the relevant output of that command (not a summary)."`
}

// NewIssuesTool lets the agent keep a ledger of problems it finds. An issue
// closes only with the command that proves it fixed and what that printed.
func NewIssuesTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		IssuesToolName,
		issuesDescription,
		func(ctx context.Context, params IssuesParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session_id is required")
			}
			ledger := IssuesFor(sessionID)
			switch strings.ToLower(strings.TrimSpace(params.Action)) {
			case "add":
				sev, err := issues.ParseSeverity(params.Severity)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				id, err := ledger.Add(params.Title, sev, params.Where)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(fmt.Sprintf("Recorded issue #%d. It stays open until closed with a command and its output.", id)), nil
			case "close":
				if err := ledger.Close(params.ID, params.Command, params.Output); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(fmt.Sprintf("Closed issue #%d.", params.ID)), nil
			case "list", "":
				return fantasy.NewTextResponse(issues.Summary(ledger.All())), nil
			}
			return fantasy.NewTextErrorResponse("action must be add, close or list"), nil
		},
	)
}

// OpenIssuesBlockingCompletion describes the serious issues still open, or
// "" when there are none. The completion gate uses it.
func OpenIssuesBlockingCompletion(sessionID string) string {
	open := IssuesFor(sessionID).OpenAtLeast(issues.Major)
	if len(open) == 0 {
		return ""
	}
	return "These issues are still open:\n" + issues.Summary(open) +
		"\nFix each one and close it with the command you ran and its output (issues tool, action close), " +
		"or tell the user plainly that it remains."
}
