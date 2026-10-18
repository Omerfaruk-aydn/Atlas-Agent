package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestLearnExportUsesGuardedSkillInvocationAndPreservesDenial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), engineering: store}
	require.NoError(t, store.Update(ctx, "s", func(st *engineering.State) error {
		st.Operations = []engineering.Operation{{ID: "observe", Tool: "view", Status: "completed", OutcomeObserved: true, FinishedAt: 1}, {ID: "test", Tool: "test_run", Status: "completed", OutcomeObserved: true, FinishedAt: 2}}
		return nil
	}))
	m, err := store.ExtractMethod(ctx, root, "s", engineering.LearnedMethod{Name: "scoped-procedure", Description: "Use for the fixture", Conditions: []string{"Fixture only"}, Versions: []string{"v1"}, Limits: []string{"Not universal"}, OperationIDs: []string{"observe", "test"}})
	require.NoError(t, err)
	calls := 0
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		require.Equal(t, tools.SkillManageToolName, call.Name)
		var p tools.SkillManageParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.Equal(t, "create", p.Action)
		require.Contains(t, p.Instructions, "Not universal")
		require.Contains(t, p.Instructions, m.ID)
		return fantasy.ToolResponse{IsError: true, StopTurn: true, Metadata: `{"permission":{"id":"pending"}}`, Content: "denied"}, nil
	}
	r, err := c.learnWorkflow(ctx, WorkflowParams{LearnAction: "export", MethodID: m.ID}, fantasy.ToolCall{ID: "export"}, invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Equal(t, `{"permission":{"id":"pending"}}`, r.Metadata)
	require.Equal(t, 1, calls)
	methods, err := store.LearnedMethods(ctx, root)
	require.NoError(t, err)
	require.Equal(t, "candidate", methods[0].Status)
	_, err = c.learnWorkflow(ctx, WorkflowParams{LearnAction: "export", MethodID: m.ID}, fantasy.ToolCall{ID: "approved"}, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("written"), nil
	})
	require.NoError(t, err)
	methods, err = store.LearnedMethods(ctx, root)
	require.NoError(t, err)
	require.Equal(t, "exported", methods[0].Status)
	require.Contains(t, methods[0].SkillPath, filepath.Join(".atlas", "skills"))
}

func TestExpertiseConflictUnloadAndMessageProjection(t *testing.T) {
	t.Parallel()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	available := []*skills.Skill{{Name: "one", SkillFilePath: "builtin://one/SKILL.md", Metadata: map[string]string{"exclusive-group": "design"}}, {Name: "two", SkillFilePath: "builtin://two/SKILL.md", Metadata: map[string]string{"exclusive-group": "design"}}}
	c := &coordinator{engineering: store, activeSkills: available, skillTracker: skills.NewTracker(available)}
	loads := 0
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		loads++
		var p tools.ViewParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		name := "one"
		if p.FilePath == available[1].SkillFilePath {
			name = "two"
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("guide "+name), tools.ViewResponseMetadata{ResourceType: tools.ViewResourceSkill, ResourceName: name, Content: "guide " + name}), nil
	}
	r, err := c.expertiseWorkflow(ctx, WorkflowParams{ExpertiseAction: "load", ExpertiseName: "one"}, fantasy.ToolCall{ID: "load-one"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError)
	r, err = c.expertiseWorkflow(ctx, WorkflowParams{ExpertiseAction: "load", ExpertiseName: "two"}, fantasy.ToolCall{ID: "load-two"}, invoke)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 1, loads)
	r, err = c.expertiseWorkflow(ctx, WorkflowParams{ExpertiseAction: "unload", ExpertiseName: "one"}, fantasy.ToolCall{ID: "unload"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError)
	st, err := store.Read(ctx, "s")
	require.NoError(t, err)
	msgs := []fantasy.Message{{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "load-one", Output: fantasy.ToolResultOutputContentText{Text: "original guide"}}}}}
	filtered := withExpertiseLifecycle(msgs, st)
	require.Equal(t, "original guide", msgs[0].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentText).Text)
	require.Contains(t, filtered[0].Content[0].(fantasy.ToolResultPart).Output.(fantasy.ToolResultOutputContentText).Text, "unloaded")
	r, err = c.expertiseWorkflow(ctx, WorkflowParams{ExpertiseAction: "load", ExpertiseName: "two"}, fantasy.ToolCall{ID: "load-two-again"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Equal(t, 2, loads)
}

func TestPatchMergePreservesPauseAndNeverReplaysPartialWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), engineering: store}
	for _, name := range []string{"one.txt", "two.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("base\n"), 0o644))
	}
	b, err := store.ForkPatchBranch(ctx, "s", root, "alternative", "", []string{"one.txt", "two.txt"})
	require.NoError(t, err)
	proposal := "changed\n"
	_, err = store.ProposeBranch(ctx, "s", b.ID, []engineering.BranchFile{{Path: "one.txt", Proposed: &proposal}, {Path: "two.txt", Proposed: &proposal}}, nil)
	require.NoError(t, err)
	edits := 0
	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if call.Name == tools.ViewToolName {
			return fantasy.NewTextResponse("read"), nil
		}
		require.Equal(t, tools.EditToolName, call.Name)
		edits++
		var p tools.EditParams
		require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
		require.Equal(t, engineering.Hash("base\n"), p.ExpectedFileHash)
		if edits == 2 {
			return fantasy.ToolResponse{IsError: true, StopTurn: true, Metadata: `{"question":{"id":"q"}}`, Content: "paused"}, nil
		}
		require.NoError(t, os.WriteFile(p.FilePath, []byte(p.NewString), 0o644))
		return fantasy.NewTextResponse("applied"), nil
	}
	r, err := c.branchWorkflow(ctx, WorkflowParams{BranchAction: "merge", BranchID: b.ID}, fantasy.ToolCall{ID: "merge"}, invoke)
	require.NoError(t, err)
	require.True(t, r.StopTurn)
	require.Contains(t, r.Metadata, "question")
	st, err := store.Read(ctx, "s")
	require.NoError(t, err)
	require.Equal(t, "interrupted", st.Branches[0].Status)
	require.Equal(t, "applied", st.Branches[0].Files[0].Dispatch)
	require.Equal(t, "unknown", st.Branches[0].Files[1].Dispatch)
	r, err = c.branchWorkflow(ctx, WorkflowParams{BranchAction: "merge", BranchID: b.ID}, fantasy.ToolCall{ID: "replay"}, invoke)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Equal(t, 2, edits)
}
