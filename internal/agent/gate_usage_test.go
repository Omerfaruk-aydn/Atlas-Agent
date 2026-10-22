package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

type traceUsageModel struct {
	fantasy.LanguageModel
	fail      bool
	onlyTotal bool
}

func TestPlanReadFailureIsMeasuredAsFailOpen(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{}
	a, env, _ := newWorkLoopAgent(t, model)
	withEvaluator(a, model, 2)
	root := t.TempDir()
	trace := &turnTrace{trace: harnessmetrics.New(func() string { return root }).BeginTrace("missing-plan")}
	ctx := context.WithValue(t.Context(), traceKey{}, trace)
	_, stop := a.reviewPlan(ctx, "missing-plan", &evaluatorLedger{root: env.workingDir}, &workplan.Plan{Goal: "fixture"})
	require.False(t, stop)
	require.Empty(t, model.calls())
	events, err := harnessmetrics.LoadTrace(root, "missing-plan", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "plan_review", events[1].Name)
	require.Equal(t, "fail_open", events[1].Outcome)
	require.Equal(t, "plan_read_failed", events[1].Reason)
}

func (m traceUsageModel) Provider() string { return "mock" }
func (m traceUsageModel) Model() string    { return "usage-model" }
func (m traceUsageModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	if m.fail {
		return nil, errors.New("unregistered-sensitive-error")
	}
	if m.onlyTotal {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop, Usage: fantasy.Usage{TotalTokens: 150}})
		}, nil
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop, Usage: fantasy.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30}})
	}, nil
}

func TestReportedTotalOnlyUsageIsNotLost(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	trace := &turnTrace{trace: harnessmetrics.New(func() string { return root }).BeginTrace("total")}
	ctx := context.WithValue(t.Context(), traceKey{}, trace)
	ctx, gate := startGate(ctx, "review")
	stream, err := observedModel(ctx, Model{Model: traceUsageModel{onlyTotal: true}}).Stream(ctx, fantasy.Call{})
	require.NoError(t, err)
	for range stream {
	}
	gate.finish("pass", "review_accepted")
	events, err := harnessmetrics.LoadTrace(root, "total", 0)
	require.NoError(t, err)
	for _, e := range events {
		if e.Kind == "model" {
			require.EqualValues(t, 150, e.Tokens())
			require.Equal(t, "unavailable", e.CostSource)
			require.False(t, e.Estimated)
		}
	}
	totals := harnessmetrics.SummarizeGates(events)
	require.Len(t, totals, 1)
	require.EqualValues(t, 150, totals[0].Tokens)
}

func TestNestedGateUsageIsAttributedOnce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	trace := &turnTrace{trace: harnessmetrics.New(func() string { return root }).BeginTrace("nested")}
	ctx := context.WithValue(t.Context(), traceKey{}, trace)
	outerCtx, outer := startGate(ctx, "outer")
	innerCtx, inner := startGate(outerCtx, "inner")
	model := observedModel(innerCtx, Model{Model: traceUsageModel{}})
	stream, err := model.Stream(innerCtx, fantasy.Call{})
	require.NoError(t, err)
	for range stream {
	}
	inner.finish("pass", "review_accepted")
	outer.finish("pass", "checks_accepted")
	events, err := harnessmetrics.LoadTrace(root, "nested", 0)
	require.NoError(t, err)
	totals := harnessmetrics.SummarizeGates(events)
	require.Len(t, totals, 2)
	require.Equal(t, "inner", totals[0].Name)
	require.EqualValues(t, 150, totals[0].Tokens)
	require.Equal(t, "outer", totals[1].Name)
	require.Zero(t, totals[1].Tokens)
	for _, e := range events {
		if e.Kind == "model" {
			require.Equal(t, "inner", e.Gate)
			require.EqualValues(t, 30, e.CacheReadTokens)
			require.False(t, e.Estimated)
		}
	}
}

func TestModelTransportErrorNeverStoresRawErrorText(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	trace := &turnTrace{trace: harnessmetrics.New(func() string { return root }).BeginTrace("error")}
	ctx := context.WithValue(t.Context(), traceKey{}, trace)
	_, err := observedModel(ctx, Model{Model: traceUsageModel{fail: true}}).Stream(ctx, fantasy.Call{})
	require.Error(t, err)
	events, err := harnessmetrics.LoadTrace(root, "error", 0)
	require.NoError(t, err)
	data, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(data), "unregistered-sensitive-error")
	require.Equal(t, "transport_error", events[len(events)-1].Reason)
}
