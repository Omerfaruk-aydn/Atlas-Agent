package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func transientModel(provider string) Model {
	model := testModel(provider)
	model.Model = &finishStreamModel{text: provider}
	model.CatwalkCfg.ContextWindow = 128000
	model.CatwalkCfg.SupportsImages = true
	model.CatwalkCfg.CanReason = true
	return model
}

func transientChain(policy *config.FallbackPolicy, fallbacks ...Model) *modelChain {
	return newModelChainWithPolicy(transientModel("primary"), fallbacks, 0, policy)
}

func TestTransientFallbackIsOptIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		policy *config.FallbackPolicy
	}{
		{name: "unset"},
		{name: "disabled", policy: &config.FallbackPolicy{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chain := transientChain(tc.policy, transientModel("other"))
			require.False(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusInternalServerError}, false))
		})
	}
}

func TestTransientFallbackAcceptsOnlySafeFailures(t *testing.T) {
	t.Parallel()
	accepted := []fantasy.ProviderError{
		{StatusCode: http.StatusRequestTimeout},
		{StatusCode: http.StatusInternalServerError},
		{StatusCode: http.StatusServiceUnavailable},
		{TransientError: true},
		{Cause: io.ErrUnexpectedEOF},
	}
	for i := range accepted {
		err := accepted[i]
		chain := transientChain(&config.FallbackPolicy{OnTransient: true}, transientModel("other"))
		require.True(t, chain.HandleRetrySafely(&err, false), "error %#v", err)
	}

	rejected := []fantasy.ProviderError{
		{StatusCode: http.StatusUnauthorized},
		{StatusCode: http.StatusBadRequest},
		{StatusCode: http.StatusInternalServerError, AuthError: true},
		{Cause: context.Canceled},
		{Cause: context.DeadlineExceeded},
		{Cause: errors.New("permanent failure")},
	}
	for i := range rejected {
		err := rejected[i]
		chain := transientChain(&config.FallbackPolicy{OnTransient: true}, transientModel("other"))
		require.False(t, chain.HandleRetrySafely(&err, false), "error %#v", err)
	}
}

func TestTransientFallbackNeverCrossesAfterOutput(t *testing.T) {
	t.Parallel()
	chain := transientChain(&config.FallbackPolicy{OnTransient: true}, transientModel("other"))
	require.False(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusBadGateway}, true))
	require.Equal(t, "primary", chain.Current().ModelCfg.Provider)
}

func TestTransientFallbackSkipsSameProviderAndIncompatibleModels(t *testing.T) {
	t.Parallel()
	sameProvider := transientModel("primary")
	noModel := transientModel("no-model")
	noModel.Model = nil
	noImages := transientModel("no-images")
	noImages.CatwalkCfg.SupportsImages = false
	noReasoning := transientModel("no-reasoning")
	noReasoning.CatwalkCfg.CanReason = false
	smallContext := transientModel("small-context")
	smallContext.CatwalkCfg.ContextWindow = 4096
	compatible := transientModel("compatible")

	chain := transientChain(&config.FallbackPolicy{OnTransient: true, MaxProviders: 4},
		sameProvider, noModel, noImages, noReasoning, smallContext, compatible)
	chain.models[0].ModelCfg.Think = true
	require.True(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusBadGateway}, false))
	require.Equal(t, compatible, chain.Current())
}

func TestTransientFallbackHasProviderBudget(t *testing.T) {
	t.Parallel()
	chain := transientChain(&config.FallbackPolicy{OnTransient: true, MaxProviders: 1},
		transientModel("second"), transientModel("third"))
	require.True(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusBadGateway}, false))
	require.False(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusBadGateway}, false))
	require.Equal(t, "second", chain.Current().ModelCfg.Provider)
}

func TestRateLimitFallbackRetainsHistoricalBehavior(t *testing.T) {
	t.Parallel()
	chain := transientChain(nil, transientModel("same-provider"))
	chain.models[1].ModelCfg.Provider = "primary"
	require.True(t, chain.HandleRetrySafely(&fantasy.ProviderError{StatusCode: http.StatusTooManyRequests}, true))
	require.Equal(t, 1, chain.active)
}
