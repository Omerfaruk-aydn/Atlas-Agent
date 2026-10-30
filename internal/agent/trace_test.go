package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestAgentTraceRecordsMockTurnWithoutPayloads(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{steps: []scriptedStep{{tool: "edit", input: `{"file_path":"a.go"}`}, {text: "private-trace-payload"}}}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool())
	sess, err := env.sessions.Create(t.Context(), "trace")
	require.NoError(t, err)
	require.NoError(t, vault.Register(sess.ID, "private-trace-payload"))
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Update the file"})
	require.NoError(t, err)
	events, err := harnessmetrics.LoadTrace(a.harness.Root(), sess.ID, 0)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Kind]++
	}
	require.Equal(t, 1, counts["turn_start"])
	require.Equal(t, 1, counts["turn_end"])
	require.Equal(t, 2, counts["model"])
	require.Equal(t, 2, counts["step_start"])
	require.Equal(t, 1, counts["tool"])
	data, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private-trace-payload")
	require.NotContains(t, string(data), "file_path")
}

func TestRetentionThroughMockSessionCompaction(t *testing.T) {
	t.Parallel()
	cases, err := evaluation.RetentionCases()
	require.NoError(t, err)
	for _, c := range cases {
		a, env, _ := newWorkLoopAgent(t, &scriptedModel{steps: []scriptedStep{{text: c.Summary}}})
		sess, err := env.sessions.Create(t.Context(), "retention")
		require.NoError(t, err)
		_, err = env.messages.Create(t.Context(), sess.ID, messageParams(c.Transcript))
		require.NoError(t, err)
		require.NoError(t, a.Summarize(t.Context(), sess.ID, nil, nil))
		updated, err := env.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		messages, err := env.messages.List(t.Context(), sess.ID)
		require.NoError(t, err)
		found := false
		for _, msg := range messages {
			if msg.ID == updated.SummaryMessageID {
				report := evaluation.ScoreRetention(c, msg.Content().Text)
				require.Equal(t, report.Total, report.Passed, c.ID)
				found = true
			}
		}
		require.True(t, found)
	}
}

func TestTraceUnavailableStorageDoesNotFailMockTurn(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{steps: []scriptedStep{{text: "done"}}})
	blocked := filepath.Join(t.TempDir(), "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("file"), 0o600))
	a.harness = harnessmetrics.New(func() string { return blocked })
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Explain the file"})
	require.NoError(t, err)
	require.Positive(t, a.harness.TraceFailures())
}

func TestTraceCanBeDisabled(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{steps: []scriptedStep{{text: "done"}}})
	disabled := false
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{Trace: &disabled}}}).Scoped(env.workingDir)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Explain the file"})
	require.NoError(t, err)
	events, err := harnessmetrics.LoadTrace(a.harness.Root(), sess.ID, 0)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestExplicitEvaluatorAblationSkipsReviewerWithoutEnablingCalls(t *testing.T) {
	t.Parallel()
	writer := &scriptedModel{steps: []scriptedStep{{tool: "edit", input: `{"file_path":"a.go"}`}, {text: "done"}}}
	reviewer := &scriptedModel{steps: []scriptedStep{{text: "VERDICT: FAIL\n- fixture"}}}
	a, env, _ := newWorkLoopAgent(t, writer, fakeEditTool())
	withEvaluator(a, reviewer, 2)
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{DisabledGates: []string{"evaluator", "contract", "advisor", "escalator"}}}}).Scoped(env.workingDir)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "Update the file"})
	require.NoError(t, err)
	require.Empty(t, reviewer.calls())
	require.False(t, a.contractGreen(sess.ID))
	require.Empty(t, a.contractGapLines(sess.ID))
	events, err := harnessmetrics.LoadTrace(a.harness.Root(), sess.ID, 0)
	require.NoError(t, err)
	found := false
	for _, e := range events {
		if e.Kind == "gate" && e.Name == "evaluator" && e.Outcome == "skipped" {
			found = true
		}
	}
	require.True(t, found)
}
