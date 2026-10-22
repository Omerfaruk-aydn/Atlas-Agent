package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func passingCheck(t *testing.T, store *engineering.Store, ctx context.Context, root string) {
	t.Helper()
	check := fantasy.NewAgentTool("test_run", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("ok"), tools.TestRunResponseMetadata{OK: true}), nil
	})
	_, err := (&guardedTool{AgentTool: check, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "t1", Name: "test_run", Input: "{}"})
	require.NoError(t, err)
}

// A job started in the background keeps writing after the check that looked at
// the tree. Nothing reported it; the next place evidence is relied on finds it.
func TestChangeAfterAPassingCheckIsFoundWhereEvidenceIsRelied(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	passingCheck(t, store, ctx, root)
	generation := effectGeneration(t, store, ctx)

	require.False(t, noteSourceDrift(ctx, store, root, "s"), "an untouched tree is not drift")
	require.Equal(t, generation, effectGeneration(t, store, ctx))

	// The background job (or an editor outside the agent) rewrites the file.
	require.NoError(t, os.WriteFile(file, []byte("package b\n"), 0o644))
	require.True(t, noteSourceDrift(ctx, store, root, "s"))
	require.Greater(t, effectGeneration(t, store, ctx), generation, "the earlier check no longer describes the tree")

	// The change is now accounted for, once.
	require.False(t, noteSourceDrift(ctx, store, root, "s"))
}

// Atlas rewrites its own plan, progress and cost files every turn. Those are
// not the project's source, so they never read as drift.
func TestAtlasBookkeepingIsNotDrift(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	passingCheck(t, store, ctx, root)
	generation := effectGeneration(t, store, ctx)

	bookkeeping := filepath.Join(root, ".atlas", "sessions", "s")
	require.NoError(t, os.MkdirAll(bookkeeping, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bookkeeping, "state.json"), []byte(`{"turns":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "work.json"), []byte(`{}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(bookkeeping, "state.json.lock"), nil, 0o600))

	require.False(t, noteSourceDrift(ctx, store, root, "s"))
	require.Equal(t, generation, effectGeneration(t, store, ctx))
}

// A change that a tracked tool already reported is not counted twice.
func TestTrackedEditIsNotCountedAsDriftAgain(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	passingCheck(t, store, ctx, root)
	edit := fantasy.NewAgentTool("edit", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("edited"), os.WriteFile(file, []byte("package c\n"), 0o644)
	})
	_, err := (&guardedTool{AgentTool: edit, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "e1", Name: "edit", Input: "{}"})
	require.NoError(t, err)
	generation := effectGeneration(t, store, ctx)

	require.False(t, noteSourceDrift(ctx, store, root, "s"))
	require.Equal(t, generation, effectGeneration(t, store, ctx))
}

// An editor tool that fails after writing part of the file reports no
// mutation; the tree still differs from what the check saw.
func TestPartialWriteBeforeFailureIsFound(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	passingCheck(t, store, ctx, root)
	generation := effectGeneration(t, store, ctx)
	failing := fantasy.NewAgentTool("write", "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		_ = os.WriteFile(file, []byte("pack"), 0o644)
		return fantasy.ToolResponse{}, errors.New("disk full")
	})
	_, _ = (&guardedTool{AgentTool: failing, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: "w1", Name: "write", Input: "{}"})
	require.Equal(t, generation, effectGeneration(t, store, ctx), "the tool itself reported nothing")

	require.True(t, noteSourceDrift(ctx, store, root, "s"))
	require.Greater(t, effectGeneration(t, store, ctx), generation)
}

// The whole path for a verified requirement: a later unreported change makes
// it stale at the point the work state is advanced.
func TestDriftStalesAVerifiedWorkItem(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(file, []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	passingCheck(t, store, ctx, root)
	var itemID string
	_, err := store.UpdateWork(ctx, "s", func(w *engineering.WorkState, st engineering.State) error {
		turn := w.RecordUserTurn("keep it building", time.UnixMilli(1000))
		item, err := w.AddItem("behavior", "package builds", engineering.OriginUser, []int{turn.Seq}, time.UnixMilli(1000))
		if err != nil {
			return err
		}
		itemID = item.ID
		return w.Advance(st, item.ID, engineering.ItemVerified, []string{"t1"}, time.UnixMilli(1000))
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(file, []byte("package z\n"), 0o644))
	require.True(t, noteSourceDrift(ctx, store, root, "s"))

	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	for _, item := range state.Work.Items {
		if item.ID == itemID {
			require.Equal(t, engineering.ItemVerified+"_stale", engineering.EffectiveStatus(item, state.Generation))
		}
	}
}

func externalCall(t *testing.T, store *engineering.Store, ctx context.Context, root string) string {
	t.Helper()
	return externalCallAs(t, store, ctx, root, "m1", "mcp_crm_update_ticket")
}

func externalCallAs(t *testing.T, store *engineering.Store, ctx context.Context, root, id, name string) string {
	t.Helper()
	inner := fantasy.NewAgentTool(name, "test", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("ok"), nil
	})
	_, err := (&guardedTool{AgentTool: inner, store: store, root: root}).Run(ctx, fantasy.ToolCall{ID: id, Name: name, Input: `{}`})
	require.NoError(t, err)
	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	for _, record := range state.Outcomes.Records {
		if record.Tool == name && record.OpID != "" && record.Phase == outcome.PhaseApplied && record.PendingExternal() {
			return record.OpID
		}
	}
	t.Fatal("no outcome record for the external call")
	return ""
}

// A tool answering "ok" for a change in another system is applied, not
// verified; it is tracked apart from the source generation and holds the
// completion until its effect is observed.
func TestExternalEffectNeedsAnObservedResult(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte("package a\n"), 0o644))
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	generation := effectGeneration(t, store, ctx)
	op := externalCall(t, store, ctx, root)
	require.Equal(t, generation, effectGeneration(t, store, ctx), "an external effect is not a source change")

	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	requirements := state.Outcomes.Requirements()
	require.Len(t, requirements, 1)
	require.Equal(t, outcome.PhaseApplied, requirements[0].Phase)
	require.Contains(t, requirements[0].ID, "external:")

	// Neither a green project contract nor a passing build covers it.
	decision := decideCompletion(unifiedInput{Requirements: requirements, ContractGreen: true})
	require.Equal(t, unverifiedVerdict, decision.Verdict, "%v", decision.Reasons)
	passingCheck(t, store, ctx, root)
	state, err = store.Read(ctx, "s")
	require.NoError(t, err)
	require.Len(t, state.Outcomes.Requirements(), 1, "a passing project check says nothing about another system")

	// Words alone do not verify another system, whoever types them.
	require.Error(t, store.Resolve(ctx, "s", op, "completed", "Fetched ticket 42: status is closed"), "free text is not an observation")
	require.Error(t, store.ResolveObserved(ctx, "s", op, "", "x"))
	require.Error(t, store.ResolveObserved(ctx, "s", op, "nope", "x"), "an unrecorded operation observes nothing")
	require.Error(t, store.ResolveObserved(ctx, "s", op, op, "x"), "an effect cannot observe itself")

	// A call to another system does not observe this one.
	other := externalCallAs(t, store, ctx, root, "m2", "mcp_billing_get_invoice")
	require.Error(t, store.ResolveObserved(ctx, "s", op, other, "Fetched an invoice"))

	// A later call to the same system proves neither which resource was read
	// nor that the requested change was observed. It must not settle either
	// call merely because its name sounds read-only.
	read := externalCallAs(t, store, ctx, root, "m3", "mcp_crm_get_ticket")
	require.Error(t, store.ResolveObserved(ctx, "s", op, read, "Fetched ticket 42: status is closed as requested"))
	mutation := externalCallAs(t, store, ctx, root, "m4", "mcp_crm_close_another_ticket")
	require.Error(t, store.ResolveObserved(ctx, "s", op, mutation, "ok"))
	state, err = store.Read(ctx, "s")
	require.NoError(t, err)
	// The call to the other system is itself an unobserved MCP call, and the
	// runtime cannot tell that it only read: it stays pending until settled.
	require.Len(t, state.Outcomes.Requirements(), 4, "opaque external effects all remain unverified")
	require.NoError(t, store.Resolve(ctx, "s", other, "abandoned", "Not part of this task"))
	state, err = store.Read(ctx, "s")
	require.NoError(t, err)
	require.NotEmpty(t, state.Outcomes.Requirements())
	decision = decideCompletion(unifiedInput{Requirements: state.Outcomes.Requirements()})
	require.False(t, decision.Complete())
}

func TestExternalEffectFoundFailedStaysOutOfVerified(t *testing.T) {
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")

	op := externalCall(t, store, ctx, root)
	require.NoError(t, store.Resolve(ctx, "s", op, "failed", "Fetched ticket 42: still open, the update never applied"))
	state, err := store.Read(ctx, "s")
	require.NoError(t, err)
	for _, record := range state.Outcomes.Records {
		if record.OpID == op {
			require.Equal(t, outcome.PhaseFailed, record.Phase)
		}
	}
}
