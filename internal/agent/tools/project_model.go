package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projectmodel"
)

// ProjectModelToolName is the name of the project model tool.
const ProjectModelToolName = "project_model"

//go:embed project_model.md
var projectModelDescription string

// ProjectModelParams are the project model tool's parameters.
type ProjectModelParams struct {
	Action    string   `json:"action" description:"summary: the project at a glance. query: one module or file, or what a name could mean. impact: what changing files reaches, with the tests to run. tests: the tests covering files. owners: who maintains a module. decisions: recorded decisions. decide: record a decision. refresh: bring the model up to date." enum:"summary,query,impact,tests,owners,decisions,decide,refresh"`
	Query     string   `json:"query,omitempty" description:"For query: a module directory, a file path or part of a path. For owners: a module or file (empty lists all). For decisions: words to filter by."`
	Files     []string `json:"files,omitempty" description:"For impact and tests: project-relative files or directories. For impact, omitting them uses the files with uncommitted changes. For decide: the paths the decision applies to."`
	Depth     int      `json:"depth,omitempty" description:"For impact: how many levels of dependents to follow, 1-6, default 3."`
	Budget    int      `json:"budget,omitempty" description:"For summary: about how many bytes to spend, default 3000, at most 12000."`
	Text      string   `json:"text,omitempty" description:"For decide: the decision, one sentence."`
	Rationale string   `json:"rationale,omitempty" description:"For decide: why it was made."`
	Force     bool     `json:"force,omitempty" description:"For refresh: rebuild everything instead of only what changed."`
}

// ProjectModelPermissionParams is what the approval dialog is given for a
// decide call.
type ProjectModelPermissionParams struct {
	FilePath  string   `json:"file_path"`
	Text      string   `json:"text"`
	Rationale string   `json:"rationale,omitempty"`
	Files     []string `json:"files,omitempty"`
}

// ProjectModelResponseMetadata describes what a project model call did.
type ProjectModelResponseMetadata struct {
	Action  string `json:"action"`
	Rebuilt bool   `json:"rebuilt,omitempty"`
}

// NewProjectModelTool answers questions from the living project model of
// the project the service covers. Every action but decide only reads; the
// model is brought up to date first, which re-reads only what changed.
func NewProjectModelTool(svc *projectmodel.Service, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ProjectModelToolName,
		projectModelDescription,
		func(ctx context.Context, p ProjectModelParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			action := strings.ToLower(strings.TrimSpace(p.Action))
			if action == "" {
				action = "summary"
			}
			switch action {
			case "summary", "query", "impact", "tests", "owners", "decisions", "decide", "refresh":
			default:
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"unknown action %q: use summary, query, impact, tests, owners, decisions, decide or refresh", p.Action)), nil
			}
			if action == "query" && strings.TrimSpace(p.Query) == "" {
				return fantasy.NewTextErrorResponse("query needs a module, a file or part of a path"), nil
			}
			if action == "tests" && len(p.Files) == 0 {
				return fantasy.NewTextErrorResponse("tests needs files: the files or directories to find tests for"), nil
			}
			if action == "decide" {
				return recordProjectDecision(ctx, svc, permissions, call, p)
			}

			m, report, err := svc.Refresh(ctx, action == "refresh" && p.Force)
			if err != nil {
				return fantasy.NewTextErrorResponse("cannot build the project model: " + err.Error()), nil
			}
			var out string
			switch action {
			case "summary":
				out = m.Overview(p.Budget)
				if out == "" {
					out = "The project has no source files the model can read."
				}
			case "query":
				out = m.Describe(p.Query)
			case "impact":
				files := p.Files
				source := ""
				if len(files) == 0 {
					files = svc.ChangedFiles(ctx)
					source = "Using the files with uncommitted changes.\n"
					if len(files) == 0 {
						return fantasy.NewTextResponse("No files were given and nothing has uncommitted changes."), nil
					}
				}
				out = source + projectmodel.FormatImpact(m.Impact(files, p.Depth))
			case "tests":
				out = m.TestsOf(p.Files)
			case "owners":
				out = m.OwnersOf(p.Query)
			case "decisions":
				out = m.DecisionsAbout(p.Query)
			case "refresh":
				out = report.String()
			}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(out),
				ProjectModelResponseMetadata{Action: action, Rebuilt: report.Rebuilt},
			), nil
		},
	)
}

// recordProjectDecision asks before it writes: a decision outlives the
// session and steers later ones.
func recordProjectDecision(ctx context.Context, svc *projectmodel.Service, permissions permission.Service, call fantasy.ToolCall, p ProjectModelParams) (fantasy.ToolResponse, error) {
	if strings.TrimSpace(p.Text) == "" {
		return fantasy.NewTextErrorResponse("decide needs text: the decision, in one sentence"), nil
	}
	sessionID := GetSessionFromContext(ctx)
	if sessionID == "" {
		return fantasy.ToolResponse{}, errors.New("session_id is required")
	}
	granted, err := permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        svc.DataDir(),
		ToolCallID:  call.ID,
		ToolName:    ProjectModelToolName,
		Action:      "decide",
		Description: "Record a project decision",
		Params: ProjectModelPermissionParams{
			FilePath:  svc.DataDir(),
			Text:      p.Text,
			Rationale: p.Rationale,
			Files:     p.Files,
		},
	})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if !granted {
		return NewPermissionDeniedResponse(permissions), nil
	}
	d, err := svc.RecordDecision(p.Text, p.Rationale, p.Files)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(fmt.Sprintf("Recorded decision %s. It appears in the project model's decisions and in impact answers for the paths it names.", d.ID)),
		ProjectModelResponseMetadata{Action: "decide"},
	), nil
}
