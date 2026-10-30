package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/stretchr/testify/require"
)

func effectGeneration(t *testing.T, store *engineering.Store, ctx context.Context) uint64 {
	t.Helper()
	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	return state.Generation
}

func bashStub(change func(), failed bool) fantasy.AgentTool {
	return fantasy.NewAgentTool("bash", "test", func(context.Context, struct {
		Command string `json:"command"`
	}, fantasy.ToolCall,
	) (fantasy.ToolResponse, error) {
		change()
		if failed {
			return fantasy.NewTextErrorResponse("exit 2"), nil
		}
		return fantasy.NewTextResponse("ok"), nil
	})
}

func TestOpaqueBashChangeStalesEarlierEvidence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	write := func() { require.NoError(t, os.WriteFile(file, []byte("package b\n"), 0o644)) }
	guard := &guardedTool{AgentTool: bashStub(write, false), store: store, root: root}
	before := effectGeneration(t, store, ctx)
	_, err := guard.Run(ctx, fantasy.ToolCall{ID: "1", Name: "bash", Input: `{"command":"sed -i s/a/b/ main.go"}`})
	require.NoError(t, err)
	require.Greater(t, effectGeneration(t, store, ctx), before, "a shell command that rewrote a source file must advance the source generation")
}

func TestOpaqueBashPartialFailureStillStalesEvidence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	write := func() { require.NoError(t, os.WriteFile(file, []byte("half"), 0o644)) }
	guard := &guardedTool{AgentTool: bashStub(write, true), store: store, root: root}
	before := effectGeneration(t, store, ctx)
	_, err := guard.Run(ctx, fantasy.ToolCall{ID: "1", Name: "bash", Input: `{"command":"gen.sh"}`})
	require.NoError(t, err)
	require.Greater(t, effectGeneration(t, store, ctx), before, "a failed command that changed the tree must still invalidate earlier checks")
}

func TestReadOnlyShellDoesNotStaleEvidence(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	guard := &guardedTool{AgentTool: bashStub(func() {}, false), store: store, root: root}
	before := effectGeneration(t, store, ctx)
	for _, command := range []string{`git status`, `ls -la`} {
		_, err := guard.Run(ctx, fantasy.ToolCall{ID: command, Name: "bash", Input: `{"command":"` + command + `"}`})
		require.NoError(t, err)
	}
	// A non-allowlisted command that leaves the tree untouched is not a change either.
	_, err := guard.Run(ctx, fantasy.ToolCall{ID: "go", Name: "bash", Input: `{"command":"go vet ./..."}`})
	require.NoError(t, err)
	require.Equal(t, before, effectGeneration(t, store, ctx), "observation must not invalidate verification")
}

func TestOpaqueMCPChangeStalesEvidence(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "gen.go")
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	inner := fantasy.NewAgentTool("mcp_srv_codegen", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("generated"), os.WriteFile(file, []byte("package gen\n"), 0o644)
	})
	before := effectGeneration(t, store, ctx)
	_, err := (&guardedTool{AgentTool: inner, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "1", Name: "mcp_srv_codegen", Input: `{}`})
	require.NoError(t, err)
	require.Greater(t, effectGeneration(t, store, ctx), before)
}

func TestBashReadOnlyRejectsShellMutationForms(t *testing.T) {
	t.Parallel()
	for command, want := range map[string]bool{
		"git status":               true,
		"ls -la":                   true,
		"git diff HEAD":            true,
		"git branch -D main":       false,
		"git tag v1":               false,
		"ls > out.txt":             false,
		"ls; rm -rf x":             false,
		"echo $(rm x)":             false,
		"kill 1":                   false,
		"go test ./...":            false,
		"timeout 5 rm x":           false,
		"git config --list":        false,
		"git config user.name foo": false,
	} {
		quoted, err := json.Marshal(command)
		require.NoError(t, err)
		input := `{"command":` + string(quoted) + `}`
		require.Equal(t, want, tools.BashReadOnly(input), command)
	}
	require.False(t, tools.BashReadOnly(`{"command":"ls","run_in_background":true}`))
}

// The end-to-end shape of F1: a requirement is verified, a shell command then
// rewrites the source, and the requirement must no longer count as verified.
func TestShellChangeAfterVerificationMakesTheRequirementStale(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	check := fantasy.NewAgentTool("test_run", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("ok"), tools.TestRunResponseMetadata{OK: true}), nil
	})
	_, err := (&guardedTool{AgentTool: check, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "t1", Name: "test_run", Input: "{}"})
	require.NoError(t, err)

	now := time.UnixMilli(1000)
	var itemID string
	_, err = store.UpdateWork(ctx, "s", func(w *engineering.WorkState, st engineering.State) error {
		turn := w.RecordUserTurn("keep the package name stable", now)
		item, err := w.AddItem("behavior", "package builds", engineering.OriginUser, []int{turn.Seq}, now)
		if err != nil {
			return err
		}
		itemID = item.ID
		return w.Advance(st, item.ID, engineering.ItemVerified, []string{"t1"}, now)
	})
	require.NoError(t, err)

	rewrite := func() { require.NoError(t, os.WriteFile(file, []byte("package renamed\n"), 0o644)) }
	_, err = (&guardedTool{AgentTool: bashStub(rewrite, false), store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "b1", Name: "bash", Input: `{"command":"sed -i s/a/renamed/ main.go"}`})
	require.NoError(t, err)

	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	var status string
	for _, item := range state.Work.Items {
		if item.ID == itemID {
			status = engineering.EffectiveStatus(item, state.Generation)
		}
	}
	require.Equal(t, engineering.ItemVerified+"_stale", status, "proof recorded before the shell edit must no longer count")
}

// A call that wrote part of its output and was then cancelled still changed
// the tree. After a restart (a new store over the same directory) the earlier
// proof must be stale and the call must not be replayed blindly.
func TestCancelledPartialWriteStalesEvidenceAcrossRestart(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "out.txt")
	dir := t.TempDir()
	store := engineering.NewStore(dir)
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), tools.SessionIDContextKey, "s"))

	partial := fantasy.NewAgentTool("bash", "test", func(callCtx context.Context, _ struct {
		Command string `json:"command"`
	}, _ fantasy.ToolCall,
	) (fantasy.ToolResponse, error) {
		require.NoError(t, os.WriteFile(file, []byte("half"), 0o644))
		cancel()
		return fantasy.ToolResponse{}, callCtx.Err()
	})
	before := effectGeneration(t, store, context.WithValue(t.Context(), tools.SessionIDContextKey, "s"))
	_, _ = (&guardedTool{AgentTool: partial, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "w1", Name: "bash", Input: `{"command":"gen-big-file"}`})

	restarted := engineering.NewStore(dir)
	fresh := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	require.Greater(t, effectGeneration(t, restarted, fresh), before, "the interrupted write advanced the generation and it survives a restart")
	state, err := restarted.Read(fresh, "s")
	require.NoError(t, err)
	require.NotEmpty(t, state.Outcomes.Unresolved(), "the interrupted mutation stays recorded as having an unknown outcome")
}
