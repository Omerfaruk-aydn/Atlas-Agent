package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type deniedExecutionPermission struct{ *mockBashPermissionService }

func (p deniedExecutionPermission) Request(context.Context, permission.CreatePermissionRequest) (bool, error) {
	return false, nil
}

type fixtureExecutionRunner struct {
	starts  int
	result  execution.Result
	request execution.Request
}

func (r *fixtureExecutionRunner) Run(context.Context, execution.Request) (execution.Result, error) {
	return r.result, nil
}

func (r *fixtureExecutionRunner) Start(_ context.Context, request execution.Request) (execution.RunHandle, error) {
	r.starts++
	r.request = request
	return execution.RunHandle{RunID: r.result.RunID, Backend: "oci"}, nil
}

func TestRecipesBashLiteralPolicyAndIsolation(t *testing.T) {
	root := t.TempDir()
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	literal := "$(touch escaped) ; echo injection ' $HOME"
	tool := newBashToolForTest(root)
	response := runBashTool(t, tool, ctx, BashParams{Argv: []string{"printf", "%s", literal}})
	require.Equal(t, literal, response.Content)
	require.True(t, ToolSucceeded("bash", response, nil))
	denied := NewBashTool(deniedExecutionPermission{}, root, &config.Attribution{TrailerStyle: config.TrailerStyleNone}, "", CommandPolicy{}, BashLimits{})
	response = runBashTool(t, denied, ctx, BashParams{Argv: []string{"printf", "%s", literal}})
	require.True(t, response.IsError)
	require.False(t, ToolOutcomeObserved("bash", response, nil))
	store := engineering.NewStore(t.TempDir())
	out, err := store.PutArtifact(t.Context(), "execution-output", []byte(literal))
	require.NoError(t, err)
	diagnostic, err := store.PutArtifact(t.Context(), "execution-error", nil)
	require.NoError(t, err)
	zero := 0
	runner := &fixtureExecutionRunner{result: execution.Result{RunID: uuid.NewString(), Done: true, ExitCode: &zero, Status: "exited", OutputRef: out, ErrorRef: diagnostic}}
	isolated := execution.WithBinding(ctx, execution.Binding{Root: root, Store: store, Factory: func(context.Context) (execution.Runner, error) { return runner, nil }})
	response = runBashTool(t, tool, isolated, BashParams{Argv: []string{"printf", "%s", literal}})
	require.False(t, response.IsError)
	require.Equal(t, []string{"printf", "%s", literal}, runner.request.Argv[5:])
	require.Equal(t, `cd "$1" && shift && exec "$@"`, runner.request.Argv[2])
	unavailable := execution.WithBinding(ctx, execution.Binding{Root: root, Store: store})
	response = runBashTool(t, tool, unavailable, BashParams{Argv: []string{"printf", "%s", literal}})
	require.True(t, response.IsError)
	require.False(t, ToolOutcomeObserved("bash", response, nil), "required isolation has no native fallback")
}

func (r *fixtureExecutionRunner) Observe(context.Context, string) (execution.Result, error) {
	return r.result, nil
}
func (r *fixtureExecutionRunner) Cancel(context.Context, string) error { return nil }

func TestExecutionBashPermissionAndObservedExit(t *testing.T) {
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	output, err := store.PutArtifact(t.Context(), "execution-output", []byte("verified output"))
	require.NoError(t, err)
	diagnostic, err := store.PutArtifact(t.Context(), "execution-error", nil)
	require.NoError(t, err)
	code := 0
	runner := &fixtureExecutionRunner{result: execution.Result{RunID: uuid.NewString(), Backend: "oci", HostOS: "windows", ExecutionOS: "linux", Done: true, ExitCode: &code, Status: "exited", OutputRef: output, ErrorRef: diagnostic}}
	factories := 0
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	ctx = execution.WithBinding(ctx, execution.Binding{Root: root, Store: store, Factory: func(context.Context) (execution.Runner, error) { factories++; return runner, nil }})
	tool := newBashToolForTest(root)
	response := runBashTool(t, tool, ctx, BashParams{Command: "printf verification"})
	require.Contains(t, response.Content, "verified output")
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.NotNil(t, metadata.Execution)
	require.Equal(t, "linux", metadata.Execution.ExecutionOS)
	require.Equal(t, &code, metadata.ExitCode)
	factories, runner.starts = 0, 0
	tool = NewBashTool(deniedExecutionPermission{}, root, &config.Attribution{TrailerStyle: config.TrailerStyleNone}, "", CommandPolicy{}, BashLimits{})
	response = runBashTool(t, tool, ctx, BashParams{Command: "printf denied"})
	require.True(t, response.IsError)
	require.Zero(t, factories)
	require.Zero(t, runner.starts)
}
