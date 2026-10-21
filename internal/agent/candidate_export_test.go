package agent

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// The contract for exporting a method that has not been measured: the
// accept_unmeasured argument is the model's request, never consent. Consent is
// what the permission service says.

func candidateFixture(t *testing.T, perms permission.Service) (*coordinator, context.Context, engineering.LearnedMethod, *int, func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) {
	t.Helper()
	root := t.TempDir()
	store := engineering.NewStore(t.TempDir())
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "s")
	c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), engineering: store, permissions: perms}
	require.NoError(t, store.Update(ctx, "s", func(st *engineering.State) error {
		st.Operations = []engineering.Operation{{ID: "observe", Tool: "view", Status: "completed", OutcomeObserved: true, FinishedAt: 1}, {ID: "test", Tool: "test_run", Status: "completed", OutcomeObserved: true, FinishedAt: 2}}
		return nil
	}))
	m, err := store.ExtractMethod(ctx, root, "s", engineering.LearnedMethod{Name: "scoped-procedure", Description: "Use for the fixture", Conditions: []string{"Fixture only"}, Versions: []string{"v1"}, Limits: []string{"Not universal"}, OperationIDs: []string{"observe", "test"}})
	require.NoError(t, err)
	writes := new(int)
	invoke := func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		*writes++
		return fantasy.NewTextResponse("written"), nil
	}
	return c, ctx, m, writes, invoke
}

func exportCandidate(t *testing.T, c *coordinator, ctx context.Context, m engineering.LearnedMethod, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) fantasy.ToolResponse {
	t.Helper()
	r, err := c.learnWorkflow(ctx, WorkflowParams{LearnAction: "export", MethodID: m.ID, AcceptUnmeasured: true}, fantasy.ToolCall{ID: "export"}, invoke)
	require.NoError(t, err)
	return r
}

func TestCandidateExportWithoutAPermissionServiceFailsClosed(t *testing.T) {
	t.Parallel()
	c, ctx, m, writes, invoke := candidateFixture(t, nil)
	r := exportCandidate(t, c, ctx, m, invoke)
	require.True(t, r.IsError, "the flag alone is not consent")
	require.Zero(t, *writes)
}

func TestCandidateExportIsDeniedInPlanMode(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), false, nil)
	perms.SetMode(permission.ModePlan)
	c, ctx, m, writes, invoke := candidateFixture(t, perms)
	r := exportCandidate(t, c, ctx, m, invoke)
	require.True(t, r.IsError)
	require.Zero(t, *writes)
}

// A blanket allowlist entry for the skill tool does not cover this export.
func TestCandidateExportNeedsItsOwnGrantDespiteSkillAllowlist(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), false, []string{tools.SkillManageToolName})
	c, ctx, m, writes, invoke := candidateFixture(t, perms)
	requests := perms.Subscribe(ctx)
	done := make(chan fantasy.ToolResponse, 1)
	go func() { done <- exportCandidate(t, c, ctx, m, invoke) }()

	select {
	case event := <-requests:
		require.Equal(t, LearnExportUnmeasuredToolName, event.Payload.ToolName, "the user is asked, whatever skill_manage allows")
		require.Contains(t, event.Payload.Description, "candidate")
		perms.Deny(event.Payload)
	case <-time.After(10 * time.Second):
		t.Fatal("no permission request was made")
	}
	r := <-done
	require.True(t, r.IsError)
	require.True(t, r.StopTurn)
	require.Zero(t, *writes, "a denied export writes nothing")
}

func TestCandidateExportGrantedByTheUserWritesACandidate(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), false, nil)
	c, ctx, m, writes, invoke := candidateFixture(t, perms)
	requests := perms.Subscribe(ctx)
	done := make(chan fantasy.ToolResponse, 1)
	go func() { done <- exportCandidate(t, c, ctx, m, invoke) }()
	select {
	case event := <-requests:
		perms.Grant(event.Payload)
	case <-time.After(10 * time.Second):
		t.Fatal("no permission request was made")
	}
	r := <-done
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, *writes)
	methods, err := c.engineering.LearnedMethods(ctx, c.cfg.WorkingDir())
	require.NoError(t, err)
	require.Equal(t, "exported_candidate", methods[0].Status)
}

// Bypass is the user's own decision to skip every prompt; it grants this as it
// grants every tool, and the result is still marked a candidate.
func TestCandidateExportInBypassModeIsStillACandidate(t *testing.T) {
	t.Parallel()
	c, ctx, m, writes, invoke := candidateFixture(t, permission.NewPermissionService(t.TempDir(), true, nil))
	r := exportCandidate(t, c, ctx, m, invoke)
	require.False(t, r.IsError, r.Content)
	require.Equal(t, 1, *writes)
	methods, err := c.engineering.LearnedMethods(ctx, c.cfg.WorkingDir())
	require.NoError(t, err)
	require.Equal(t, "exported_candidate", methods[0].Status)
}

func TestUnmeasuredExportWithoutTheFlagNeverAsks(t *testing.T) {
	t.Parallel()
	perms := permission.NewPermissionService(t.TempDir(), false, nil)
	c, ctx, m, writes, invoke := candidateFixture(t, perms)
	r, err := c.learnWorkflow(ctx, WorkflowParams{LearnAction: "export", MethodID: m.ID}, fantasy.ToolCall{ID: "export"}, invoke)
	require.NoError(t, err)
	require.True(t, r.IsError)
	require.Zero(t, *writes)
}

func TestLearnWorkflowRecordsFailureOutcome(t *testing.T) {
	t.Parallel()
	c, ctx, m, _, invoke := candidateFixture(t, nil)
	r, err := c.learnWorkflow(ctx, WorkflowParams{LearnAction: "outcome", MethodID: m.ID, MethodOutcome: "failure"}, fantasy.ToolCall{ID: "outcome"}, invoke)
	require.NoError(t, err)
	require.False(t, r.IsError)
	stored, err := c.engineering.CurrentMethod(ctx, c.cfg.WorkingDir(), m.ID)
	require.NoError(t, err)
	require.Len(t, stored.Outcomes, 1)
	require.Equal(t, "s", stored.Outcomes[0].TaskID)
	require.False(t, stored.Outcomes[0].Success)
}

func TestUnreliableExportNeverWritesEvenWithBypass(t *testing.T) {
	t.Parallel()
	c, ctx, m, writes, invoke := candidateFixture(t, permission.NewPermissionService(t.TempDir(), true, nil))
	for _, task := range []string{"failed-a", "failed-b", "failed-c"} {
		_, err := c.engineering.RecordMethodOutcome(ctx, c.cfg.WorkingDir(), m.ID, task, false)
		require.NoError(t, err)
	}
	r := exportCandidate(t, c, ctx, m, invoke)
	require.True(t, r.IsError)
	require.Contains(t, r.Content, "unreliable")
	require.Zero(t, *writes, "the refusal must precede skill_manage, including in bypass mode")
}
