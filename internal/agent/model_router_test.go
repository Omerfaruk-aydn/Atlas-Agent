package agent

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/modelrouter"
)

type stubLanguageModel struct{ fantasy.LanguageModel }

func routerTestModel(provider, id string, in, out float64) Model {
	return Model{
		Model:      stubLanguageModel{},
		ModelCfg:   config.SelectedModel{Provider: provider, Model: id},
		CatwalkCfg: catwalk.Model{ID: id, CostPer1MIn: in, CostPer1MOut: out, ContextWindow: 200_000},
	}
}

func toolResultStep(name string, result fantasy.ToolResultOutputContent) fantasy.StepResult {
	return fantasy.StepResult{Response: fantasy.Response{Content: fantasy.ResponseContent{
		fantasy.ToolResultContent{ToolName: name, Result: result},
	}}}
}

func TestRouterStepsMarksErrorsAndClipsOutput(t *testing.T) {
	t.Parallel()
	long := make([]byte, routerOutputClip*2)
	for i := range long {
		long[i] = 'x'
	}
	steps := routerSteps([]fantasy.StepResult{
		toolResultStep("bash", fantasy.ToolResultOutputContentText{Text: string(long)}),
		toolResultStep("edit", fantasy.ToolResultOutputContentError{Error: errors.New("no match")}),
	})
	require.Len(t, steps, 2)
	require.False(t, steps[0].Calls[0].Failed)
	require.Len(t, steps[0].Calls[0].Output, routerOutputClip)
	require.True(t, steps[1].Calls[0].Failed)
}

func TestRouteStepSwitchesAndFailoverWins(t *testing.T) {
	t.Parallel()
	large := routerTestModel("anthropic", "big", 15, 75)
	fast := routerTestModel("anthropic", "small", 1, 5)
	fallback := routerTestModel("openai", "other", 2, 8)
	a := &sessionAgent{}
	a.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: fast})

	chain := newModelChain(large, []Model{fallback}, 0)
	a.routeStep("s", "list the files in internal", chain, large, fantasy.PrepareStepFunctionOptions{}, 1000, false)
	require.Equal(t, "small", chain.Current().ModelCfg.Model)

	// A 429 failover beats routing for the rest of the turn.
	require.True(t, chain.HandleRetry(&fantasy.ProviderError{StatusCode: http.StatusTooManyRequests}))
	require.Equal(t, "other", chain.Current().ModelCfg.Model)
	a.routeStep("s", "list the files in internal", chain, large, fantasy.PrepareStepFunctionOptions{StepNumber: 1}, 1000, false)
	require.Equal(t, "other", chain.Current().ModelCfg.Model)
}

func TestRouteStepDisabledPaths(t *testing.T) {
	t.Parallel()
	large := routerTestModel("anthropic", "big", 15, 75)
	fast := routerTestModel("anthropic", "small", 1, 5)
	opts := fantasy.PrepareStepFunctionOptions{}

	// No router installed.
	chain := newModelChain(large, nil, 0)
	(&sessionAgent{}).routeStep("s", "list files", chain, large, opts, 0, false)
	require.Equal(t, "big", chain.Current().ModelCfg.Model)

	// Sub-agents are never routed.
	sub := &sessionAgent{isSubAgent: true}
	sub.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: fast})
	sub.routeStep("s", "list files", chain, large, opts, 0, false)
	require.Equal(t, "big", chain.Current().ModelCfg.Model)

	// A system prompt prefix is tied to the session's provider.
	a := &sessionAgent{}
	a.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: fast})
	a.routeStep("s", "list files", chain, large, opts, 0, true)
	require.Equal(t, "big", chain.Current().ModelCfg.Model)

	// Fast model identical to the strong one.
	same := &sessionAgent{}
	same.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: large})
	same.routeStep("s", "list files", chain, large, opts, 0, false)
	require.Equal(t, "big", chain.Current().ModelCfg.Model)
}

func TestRouteStepStrongRoleAndEscalation(t *testing.T) {
	t.Parallel()
	large := routerTestModel("anthropic", "session", 3, 15)
	fast := routerTestModel("anthropic", "small", 1, 5)
	strong := routerTestModel("anthropic", "opus", 15, 75)
	a := &sessionAgent{}
	a.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: fast, strong: &strong})
	chain := newModelChain(large, nil, 0)

	a.routeStep("s", "debug the failing tests", chain, large, fantasy.PrepareStepFunctionOptions{}, 1000, false)
	require.Equal(t, "opus", chain.Current().ModelCfg.Model, "strong role replaces the session model on strong steps")

	a.routeStep("s", "show readme", chain, large, fantasy.PrepareStepFunctionOptions{}, 1000, false)
	require.Equal(t, "small", chain.Current().ModelCfg.Model)

	failed := toolResultStep("edit", fantasy.ToolResultOutputContentError{Error: errors.New("boom")})
	a.routeStep("s", "show readme", chain, large, fantasy.PrepareStepFunctionOptions{
		StepNumber: 2, Steps: []fantasy.StepResult{failed, failed},
	}, 1000, false)
	require.Equal(t, "opus", chain.Current().ModelCfg.Model)
}

func TestRouterStepSaving(t *testing.T) {
	t.Parallel()
	strong := routerTestModel("p", "big", 10, 50)
	fast := routerTestModel("p", "small", 1, 5)
	usage := fantasy.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}
	// Strong: 10 + 5 = 15; fast: 1 + 0.5 = 1.5.
	require.InDelta(t, 13.5, routerStepSaving(fast, strong, usage), 1e-9)

	strong.FlatRate = true
	require.Zero(t, routerStepSaving(fast, strong, usage))
	strong.FlatRate = false
	fast.FlatRate = true
	require.InDelta(t, 15.0, routerStepSaving(fast, strong, usage), 1e-9)
}

func TestNoteRouterStepRecordsOnlyFastSteps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	large := routerTestModel("p", "big", 10, 50)
	fast := routerTestModel("p", "small", 1, 5)
	a := &sessionAgent{harness: harnessmetrics.New(func() string { return dir })}
	a.SetRouter(&routerRuntime{router: modelrouter.New(modelrouter.Config{}), fast: fast})
	usage := fantasy.Usage{InputTokens: 1_000_000}

	chain := newModelChain(large, nil, 0)
	a.noteRouterStep("s", chain, large, usage, false) // Not routed yet.
	chain.Route(&fast)
	a.noteRouterStep("s", chain, large, usage, true) // Estimated usage.
	a.noteRouterStep("s", chain, large, usage, false)

	events, err := harnessmetrics.Load(dir, timeZero())
	require.NoError(t, err)
	totals := harnessmetrics.RouterSavings(events, "s")
	require.Equal(t, 1, totals.RoutedSteps)
	require.InDelta(t, 9.0, totals.Saved, 1e-6)
}

func TestHasImageParts(t *testing.T) {
	t.Parallel()
	require.False(t, hasImageParts([]fantasy.Message{fantasy.NewUserMessage("hi")}))
	img := fantasy.NewUserMessage("look", fantasy.FilePart{MediaType: "image/png", Data: []byte{1}})
	require.True(t, hasImageParts([]fantasy.Message{img}))
}
