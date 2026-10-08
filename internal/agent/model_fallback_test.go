package agent

import (
	"net/http"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func testModel(provider string) Model {
	return Model{ModelCfg: config.SelectedModel{Provider: provider, Model: provider + "-model"}}
}

func TestModelChainStartsOnThePrimary(t *testing.T) {
	t.Parallel()
	primary := testModel("anthropic")
	chain := newModelChain(primary, []Model{testModel("openai")}, 0)

	require.Equal(t, primary, chain.Current())
	require.False(t, chain.Fellback())
}

func TestModelChainRefreshPreservesSelectedFallback(t *testing.T) {
	t.Parallel()
	primary := testModel("claude")
	primary.Model = &finishStreamModel{text: "old primary"}
	fallback := testModel("antigravity")
	fallback.Model = &finishStreamModel{text: "old fallback"}
	chain := newModelChain(primary, []Model{fallback}, 1)
	refreshed := fallback
	refreshed.Model = &finishStreamModel{text: "fresh fallback"}
	chain.RefreshModels(primary, []Model{refreshed})
	require.Same(t, refreshed.Model, chain.Provider())
	require.True(t, chain.Fellback())
	// A user's new model selection must not replace this turn's identity.
	unrelated := testModel("openai")
	unrelated.Model = &finishStreamModel{text: "unrelated selection"}
	chain.RefreshModels(unrelated, nil)
	require.Same(t, refreshed.Model, chain.Provider())
	require.Equal(t, fallback.ModelCfg, chain.Current().ModelCfg)
}

func TestModelChainMovesOnRateLimit(t *testing.T) {
	t.Parallel()
	fallback := testModel("openai")
	chain := newModelChain(testModel("anthropic"), []Model{fallback}, 0)

	moved := chain.HandleRetry(&fantasy.ProviderError{StatusCode: http.StatusTooManyRequests})

	require.True(t, moved)
	require.True(t, chain.Fellback())
	require.Equal(t, fallback, chain.Current())
}

func TestModelChainIgnoresOtherErrors(t *testing.T) {
	t.Parallel()
	chain := newModelChain(testModel("anthropic"), []Model{testModel("openai")}, 0)

	for _, err := range []*fantasy.ProviderError{
		{StatusCode: http.StatusInternalServerError},
		{StatusCode: http.StatusUnauthorized},
		nil,
	} {
		moved := chain.HandleRetry(err)
		require.False(t, moved, "status %v must not trigger a failover", err)
	}
	require.False(t, chain.Fellback())
}

func TestModelChainWalksTheWholeList(t *testing.T) {
	t.Parallel()
	second := testModel("openai")
	third := testModel("groq")
	chain := newModelChain(testModel("anthropic"), []Model{second, third}, 0)

	rateLimited := &fantasy.ProviderError{StatusCode: http.StatusTooManyRequests}

	require.True(t, chain.HandleRetry(rateLimited))
	require.Equal(t, second, chain.Current())

	require.True(t, chain.HandleRetry(rateLimited))
	require.Equal(t, third, chain.Current())
}

func TestModelChainStopsAtTheEndOfTheList(t *testing.T) {
	t.Parallel()
	chain := newModelChain(testModel("anthropic"), nil, 0)

	moved := chain.HandleRetry(&fantasy.ProviderError{StatusCode: http.StatusTooManyRequests})

	require.False(t, moved, "no fallback is configured, so a 429 has nowhere to go")
	require.False(t, chain.Fellback())
}

func TestModelChainNeverMovesBackward(t *testing.T) {
	t.Parallel()
	second := testModel("openai")
	chain := newModelChain(testModel("anthropic"), []Model{second}, 0)
	rateLimited := &fantasy.ProviderError{StatusCode: http.StatusTooManyRequests}

	require.True(t, chain.HandleRetry(rateLimited))
	require.Equal(t, second, chain.Current())

	// A further 429 -- now against the fallback, which has nowhere further
	// to go -- must not wrap back around to the primary.
	require.False(t, chain.HandleRetry(rateLimited))
	require.Equal(t, second, chain.Current())
}

func TestModelChainCanStartOnAFallback(t *testing.T) {
	t.Parallel()
	second := testModel("openai")
	chain := newModelChain(testModel("anthropic"), []Model{second}, 1)

	require.Equal(t, second, chain.Current())
	require.True(t, chain.Fellback())
}

// A stale sticky index -- from a fallback list that has since shrunk --
// must not panic or silently clamp to the last model; it starts over on the
// primary instead.
func TestModelChainTreatsAnOutOfRangeStartIndexAsPrimary(t *testing.T) {
	t.Parallel()
	primary := testModel("anthropic")
	chain := newModelChain(primary, []Model{testModel("openai")}, 5)

	require.Equal(t, primary, chain.Current())
	require.False(t, chain.Fellback())
}

func TestModelChainRejectsANegativeStartIndex(t *testing.T) {
	t.Parallel()
	primary := testModel("anthropic")
	chain := newModelChain(primary, []Model{testModel("openai")}, -1)

	require.Equal(t, primary, chain.Current())
}

func TestStickyFallbackActiveIndexBeforeExpiry(t *testing.T) {
	t.Parallel()
	s := stickyFallback{index: 2, until: time.Now().Add(time.Minute)}
	require.Equal(t, 2, s.activeIndex(time.Now()))
}

func TestStickyFallbackActiveIndexAfterExpiry(t *testing.T) {
	t.Parallel()
	s := stickyFallback{index: 2, until: time.Now().Add(-time.Minute)}
	require.Equal(t, 0, s.activeIndex(time.Now()))
}

func TestStickyFallbackZeroValueIsAlwaysThePrimary(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, stickyFallback{}.activeIndex(time.Now()))
}
