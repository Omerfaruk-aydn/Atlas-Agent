package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/knowledge"
)

//go:embed knowledge.md
var knowledgeDescription string

const KnowledgeToolName = "knowledge"

type KnowledgeParams struct {
	Action  string   `json:"action" description:"propose, list or read."`
	ID      string   `json:"id,omitempty" description:"For read: the item id."`
	Title   string   `json:"title,omitempty" description:"For propose: a short title."`
	Summary string   `json:"summary,omitempty" description:"For propose: one line, at most 240 characters, saying what the note tells a future reader."`
	Body    string   `json:"body,omitempty" description:"For propose: the note itself, at most 16 KiB."`
	Sources []string `json:"sources,omitempty" description:"For propose: project-relative files the note was derived from. The note is flagged stale when they change."`
}

// NewKnowledgeTool lets the agent propose and read project knowledge. It has
// no way to approve: only the user does, with `atlas knowledge approve`.
func NewKnowledgeTool(store *knowledge.Store) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		KnowledgeToolName,
		knowledgeDescription,
		func(ctx context.Context, params KnowledgeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch strings.ToLower(strings.TrimSpace(params.Action)) {
			case "propose":
				it, err := store.Propose(params.Title, params.Summary, params.Body, params.Sources)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(fmt.Sprintf("Proposed %q. It is not part of the project's standing context until the user approves it (atlas knowledge approve %s).", it.ID, it.ID)), nil
			case "list":
				items, err := store.List()
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if len(items) == 0 {
					return fantasy.NewTextResponse("No knowledge items."), nil
				}
				var b strings.Builder
				for _, it := range items {
					fmt.Fprintf(&b, "- %s [%s]: %s -- %s", it.ID, it.Status, it.Title, it.Summary)
					if stale := store.Stale(it); len(stale) > 0 {
						fmt.Fprintf(&b, " [STALE: %s]", strings.Join(stale, ", "))
					}
					b.WriteString("\n")
				}
				return fantasy.NewTextResponse(strings.TrimRight(b.String(), "\n")), nil
			case "read":
				it, err := store.Get(params.ID)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				out := fmt.Sprintf("# %s [%s]\n%s\n", it.Title, it.Status, it.Body)
				if stale := store.Stale(it); len(stale) > 0 {
					out += "\nSTALE: " + strings.Join(stale, ", ") + " changed since this note was written. Verify it against the code before relying on it.\n"
				}
				return fantasy.NewTextResponse(out), nil
			}
			return fantasy.NewTextErrorResponse("action must be propose, list or read"), nil
		},
	)
}
