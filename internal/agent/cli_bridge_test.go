package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// TestCLIBridgeAgentFixture is the real program run by the hook tests: it
// creates the file named after the marker argument.
func TestCLIBridgeAgentFixture(t *testing.T) {
	i := slices.Index(os.Args, "cli-bridge-agent-fixture")
	if i < 0 || i+1 >= len(os.Args) {
		t.Skip("helper process for CLI Bridge hook tests")
	}
	if err := os.WriteFile(os.Args[i+1], []byte("ran"), 0o600); err != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

type bridgeHookHarness struct {
	root  string
	store *engineering.Store
	tool  fantasy.AgentTool
	ctx   context.Context
}

// newBridgeHookHarness wires cli_bridge the way the coordinator does: the
// nested bash call is hooked and guarded, and so is cli_bridge itself.
func newBridgeHookHarness(t *testing.T, hookCommand string) *bridgeHookHarness {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	exe, err := os.Executable()
	require.NoError(t, err)
	profile, err := json.Marshal(map[string]any{
		"kind": clibridge.ProfileKind, "version": 1, "id": "marker", "description": "Create a marker file.",
		"executable": map[string]any{"path": exe},
		"operations": []any{map[string]any{
			"id": "touch", "description": "Create the marker.",
			"args":    []any{"-test.run=^TestCLIBridgeAgentFixture$", "--", "cli-bridge-agent-fixture", "{output}"},
			"params":  []any{map[string]any{"name": "output", "type": "output_file", "required": true}},
			"outputs": []any{map[string]any{"param": "output", "min_bytes": 1}},
		}},
	})
	require.NoError(t, err)
	profiles := clibridge.Store{ProjectDir: clibridge.ProjectProfilesDir(root), UserDir: filepath.Join(t.TempDir(), "user")}
	_, err = profiles.Import(t.Context(), profile, clibridge.ImportOptions{})
	require.NoError(t, err)

	store := engineering.NewStore(t.TempDir())
	perms := permission.NewPermissionService(root, true, nil)
	bash := tools.NewBashTool(perms, root, &config.Attribution{TrailerStyle: config.TrailerStyleNone}, "", tools.CommandPolicy{}, tools.BashLimits{})
	var pre *hooks.Runner
	if hookCommand != "" {
		pre = newRunner(t, hookCommand)
	}
	hookedBash := &guardedTool{AgentTool: newHookedTool(bash, pre, nil), store: store}
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if call.Name != tools.BashToolName {
			return fantasy.NewTextErrorResponse("unavailable"), nil
		}
		return hookedBash.Run(ctx, call)
	}
	bridge := tools.NewCLIBridgeTool(tools.CLIBridgeServices{Root: root, Store: store, Permissions: perms, Profiles: profiles, Invoke: invoke})
	return &bridgeHookHarness{
		root: root, store: store,
		tool: &guardedTool{AgentTool: newHookedTool(bridge, pre, nil), store: store},
		ctx:  context.WithValue(t.Context(), tools.SessionIDContextKey, "bridge-hooks"),
	}
}

func (h *bridgeHookHarness) run(t *testing.T, action string) fantasy.ToolResponse {
	t.Helper()
	input, _ := json.Marshal(tools.CLIBridgeParams{Action: action, Profile: "marker", Operation: "touch", Args: []string{"output=marker.txt"}})
	resp, err := h.tool.Run(h.ctx, fantasy.ToolCall{ID: "bridge-" + action, Name: tools.CLIBridgeToolName, Input: string(input)})
	require.NoError(t, err)
	return resp
}

func TestCLIBridgeHookDenialOfNestedBashStartsNoProcess(t *testing.T) {
	t.Parallel()
	// The hook allows cli_bridge itself and denies the literal command it
	// dispatches, proving the nested bash call keeps its own hook check.
	h := newBridgeHookHarness(t, `read -r payload; case "$payload" in *'"tool_name":"bash"'*) echo '{"decision":"deny","reason":"no external programs"}' ;; esac`)
	resp := h.run(t, "run")
	require.True(t, resp.IsError, resp.Content)
	require.False(t, resp.StopTurn)
	var report clibridge.RunReport
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &report), resp.Content)
	require.Equal(t, clibridge.OutcomeHookDenied, report.Outcome)
	require.Contains(t, report.Detail, "no external programs")
	_, err := os.Stat(filepath.Join(h.root, "marker.txt"))
	require.True(t, os.IsNotExist(err), "the hook ran before any process")
}

func TestCLIBridgeHookHaltStopsTheTurn(t *testing.T) {
	t.Parallel()
	h := newBridgeHookHarness(t, `read -r payload; case "$payload" in *'"tool_name":"bash"'*) echo '{"decision":"deny","halt":true,"reason":"stop everything"}' ;; esac`)
	resp := h.run(t, "run")
	require.True(t, resp.StopTurn, "a halting hook still ends the turn through cli_bridge")
	_, err := os.Stat(filepath.Join(h.root, "marker.txt"))
	require.True(t, os.IsNotExist(err))
}

func TestCLIBridgeAccountingSeparatesPreviewFromObservedRuns(t *testing.T) {
	t.Parallel()
	h := newBridgeHookHarness(t, "")
	preview := h.run(t, "preview")
	require.False(t, preview.IsError, preview.Content)
	state, err := h.store.Read(h.ctx, "bridge-hooks")
	require.NoError(t, err)
	require.Len(t, state.Operations, 1)
	require.Equal(t, "completed", state.Operations[0].Status, "a preview is a successful call")
	require.False(t, state.Operations[0].OutcomeObserved, "but not an observed outcome")
	require.Empty(t, state.Failures)

	run := h.run(t, "run")
	require.False(t, run.IsError, run.Content)
	require.FileExists(t, filepath.Join(h.root, "marker.txt"))
	state, err = h.store.Read(h.ctx, "bridge-hooks")
	require.NoError(t, err)
	var observed []string
	for _, op := range state.Operations {
		if op.OutcomeObserved {
			observed = append(observed, op.Tool)
		}
	}
	require.ElementsMatch(t, []string{tools.BashToolName, tools.CLIBridgeToolName}, observed, "the nested bash call and the bridge run are both recorded")
	require.True(t, engineering.IsVerificationTool(tools.CLIBridgeToolName))
}
