package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/environment"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentWorkflowInspectUsesNoCommands(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.26.6\n"), 0o644))
	sess, err := env.sessions.Create(t.Context(), "environment")
	require.NoError(t, err)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), engineering: engineering.NewStore(t.TempDir()), sessions: env.sessions, permissions: env.permissions}
	calls := 0
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		return fantasy.NewTextResponse("unobserved"), nil
	}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	response, err := c.workflowTool(invoke).Run(ctx, fantasy.ToolCall{ID: "inspect", Name: "workflow", Input: `{"action":"environment","environment_action":"inspect"}`})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Contains(t, response.Content, "source_fingerprint")
	require.Zero(t, calls)
}

func TestEnvironmentApplyStaleDeniedAndMissingVersion(t *testing.T) {
	for _, mode := range []string{"stale", "tampered", "denied", "failed-install", "missing-version", "passed"} {
		t.Run(mode, func(t *testing.T) {
			env := testEnv(t)
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.26.6\n"), 0o644))
			sess, err := env.sessions.Create(t.Context(), "environment")
			require.NoError(t, err)
			c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: engineering.NewStore(t.TempDir())}
			ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
			calls := 0
			invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				calls++
				var params tools.BashParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &params))
				code, output := 0, ""
				if mode == "failed-install" {
					code = 1
				}
				if mode != "missing-version" && strings.Contains(params.Command, "version") {
					output = "go version go1.26.6 windows/amd64"
				}
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(output), tools.BashResponseMetadata{ExitCode: &code, Output: output}), nil
			}
			response, err := c.environmentWorkflow(ctx, WorkflowParams{EnvironmentAction: "inspect"}, fantasy.ToolCall{ID: "inspect"}, invoke)
			require.NoError(t, err)
			require.False(t, response.IsError)
			var inspection struct {
				Plan environment.EnvironmentPlan `json:"plan"`
			}
			require.NoError(t, json.Unmarshal([]byte(response.Content), &inspection))
			if mode == "stale" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module revised\ngo 1.26.6\n"), 0o644))
			}
			if mode == "tampered" {
				inspection.Plan.Commands[0].Argv = []string{"go", "run", "attacker.go"}
			}
			env.permissions.SetMode(permission.ModeBypass)
			if mode == "denied" {
				env.permissions.SetMode(permission.ModePlan)
			}
			response, err = c.environmentWorkflow(ctx, WorkflowParams{EnvironmentAction: "apply", EnvironmentPlan: &inspection.Plan}, fantasy.ToolCall{ID: "apply"}, invoke)
			require.NoError(t, err)
			if mode == "stale" || mode == "tampered" || mode == "denied" {
				require.True(t, response.IsError)
				require.Zero(t, calls)
				return
			}
			var report environment.EnvironmentReport
			require.NoError(t, json.Unmarshal([]byte(response.Content), &report))
			require.Equal(t, mode == "passed", report.Passed)
			require.Positive(t, calls)
			if mode != "passed" {
				require.NotEmpty(t, report.Gaps)
			}
		})
	}
}

func TestEnvironmentCancellationRequiresFreshInspection(t *testing.T) {
	env := testEnv(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\ngo 1.26.6\n"), 0o644))
	sess, err := env.sessions.Create(t.Context(), "environment")
	require.NoError(t, err)
	env.permissions.SetMode(permission.ModeBypass)
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: engineering.NewStore(t.TempDir())}
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
	response, err := c.environmentWorkflow(ctx, WorkflowParams{EnvironmentAction: "inspect"}, fantasy.ToolCall{ID: "inspect"}, nil)
	require.NoError(t, err)
	var inspection struct {
		Plan environment.EnvironmentPlan `json:"plan"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &inspection))
	cancelCtx, cancel := context.WithCancel(ctx)
	calls := 0
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		cancel()
		return fantasy.ToolResponse{}, context.Canceled
	}
	_, err = c.environmentWorkflow(cancelCtx, WorkflowParams{EnvironmentAction: "apply", EnvironmentPlan: &inspection.Plan}, fantasy.ToolCall{ID: "cancel"}, invoke)
	require.ErrorIs(t, err, context.Canceled)
	response, err = c.environmentWorkflow(ctx, WorkflowParams{EnvironmentAction: "apply", EnvironmentPlan: &inspection.Plan}, fantasy.ToolCall{ID: "replay"}, invoke)
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "fresh environment inspection")
	require.Equal(t, 1, calls)
}

func TestEnvironmentCommandQuotesShellMetacharacters(t *testing.T) {
	t.Parallel()
	command := environmentCommand(environment.Command{Argv: []string{"printf", "%s", "literal'; touch unexpected; '"}, Env: []string{"GOTOOLCHAIN=local"}})
	require.Equal(t, `GOTOOLCHAIN='local' 'printf' '%s' 'literal'"'"'; touch unexpected; '"'"''`, command)
}
