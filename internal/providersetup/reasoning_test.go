package providersetup_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/providersetup"
	"github.com/stretchr/testify/require"
)

// entriesServer serves the given raw /v1/models entries.
func entriesServer(t *testing.T, entries ...map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": entries})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func probeModels(t *testing.T, entries ...map[string]any) map[string]catwalk.Model {
	t.Helper()
	srv := entriesServer(t, entries...)
	res, err := providersetup.Probe(t.Context(), nil, catwalk.TypeOpenAICompat, srv.URL+"/v1", testKey)
	require.NoError(t, err)
	out := map[string]catwalk.Model{}
	for _, m := range res.Models {
		out[m.ID] = m
	}
	return out
}

func TestReasoningFromOpenRouterSupportedParameters(t *testing.T) {
	t.Parallel()
	got := probeModels(t,
		map[string]any{"id": "x/think", "supported_parameters": []string{"tools", "reasoning", "include_reasoning"}},
		map[string]any{"id": "x/effort", "supported_parameters": []string{"reasoning_effort"}},
		map[string]any{"id": "x/plain", "supported_parameters": []string{"tools", "temperature"}},
	)
	for _, id := range []string{"x/think", "x/effort"} {
		require.True(t, got[id].CanReason, id)
		require.Equal(t, []string{"low", "medium", "high"}, got[id].ReasoningLevels, id)
		require.Equal(t, "medium", got[id].DefaultReasoningEffort, id)
	}
	require.False(t, got["x/plain"].CanReason)
	require.Empty(t, got["x/plain"].ReasoningLevels)
	require.Empty(t, got["x/plain"].DefaultReasoningEffort)
}

func TestReasoningBooleanFlagsWithAndWithoutLevels(t *testing.T) {
	t.Parallel()
	got := probeModels(t,
		map[string]any{"id": "b/flag", "supports_reasoning": true},
		map[string]any{"id": "b/caps", "capabilities": map[string]any{"thinking": true}},
		map[string]any{"id": "b/levels", "reasoning": true, "reasoning_efforts": []string{"high", "low"}},
		map[string]any{"id": "b/obj", "thinking": map[string]any{"supported": true, "levels": []string{"minimal", "high"}}},
		map[string]any{"id": "b/off", "can_reason": false},
	)
	require.True(t, got["b/flag"].CanReason)
	require.Equal(t, []string{"low", "medium", "high"}, got["b/flag"].ReasoningLevels)
	require.True(t, got["b/caps"].CanReason)
	require.Equal(t, "medium", got["b/caps"].DefaultReasoningEffort)

	require.True(t, got["b/levels"].CanReason)
	require.Equal(t, []string{"low", "high"}, got["b/levels"].ReasoningLevels)
	require.Equal(t, "high", got["b/levels"].DefaultReasoningEffort)

	require.Equal(t, []string{"minimal", "high"}, got["b/obj"].ReasoningLevels)
	require.Equal(t, "high", got["b/obj"].DefaultReasoningEffort)

	require.False(t, got["b/off"].CanReason)
	require.Empty(t, got["b/off"].ReasoningLevels)
}

func TestReasoningUnknownLevelsAreIgnored(t *testing.T) {
	t.Parallel()
	got := probeModels(t,
		map[string]any{"id": "u/mixed", "supported_reasoning_efforts": []string{"turbo", "Medium", "ultra", "low", "low"}},
		map[string]any{"id": "u/onlyjunk", "reasoning": true, "reasoning_levels": []string{"turbo"}},
		map[string]any{"id": "u/shape", "reasoning": "yes", "reasoning_efforts": "high"},
	)
	require.Equal(t, []string{"low", "medium"}, got["u/mixed"].ReasoningLevels)
	require.Equal(t, "medium", got["u/mixed"].DefaultReasoningEffort)
	// Reasoning is claimed but no level is usable: standard defaults.
	require.Equal(t, []string{"low", "medium", "high"}, got["u/onlyjunk"].ReasoningLevels)
	// Unrecognised shapes are silence, never support.
	require.False(t, got["u/shape"].CanReason)
}

func catalogReasoner(t *testing.T) catwalk.Model {
	t.Helper()
	for _, p := range embedded.GetAll() {
		for _, m := range p.Models {
			if m.CanReason && len(m.ReasoningLevels) > 0 && m.DefaultReasoningEffort != "" {
				return m
			}
		}
	}
	t.Skip("no reasoning model in the embedded catalog")
	return catwalk.Model{}
}

func TestReasoningCatalogFallbackOnExactIDOnly(t *testing.T) {
	t.Parallel()
	cm := catalogReasoner(t)
	got := probeModels(t,
		map[string]any{"id": cm.ID},
		map[string]any{"id": cm.ID + "-nope"},
		map[string]any{"id": cm.ID + "-denied", "can_reason": false},
	)
	require.True(t, got[cm.ID].CanReason)
	require.NotEmpty(t, got[cm.ID].ReasoningLevels)
	require.Contains(t, got[cm.ID].ReasoningLevels, got[cm.ID].DefaultReasoningEffort)
	require.False(t, got[cm.ID+"-nope"].CanReason)
	require.False(t, got[cm.ID+"-denied"].CanReason)
}

func TestSetupWritesReasoningFieldsAndSummary(t *testing.T) {
	cm := catalogReasoner(t)
	srv := entriesServer(t,
		map[string]any{"id": "r/one", "supported_parameters": []string{"reasoning"}},
		map[string]any{"id": "r/plain"},
	)
	store := newStore(t)
	p := baseParams(srv.URL + "/v1")
	p.Models = []string{cm.ID, "custom-explicit"}

	res, err := providersetup.Setup(t.Context(), store, p, fixedKey(testKey))
	require.NoError(t, err)
	require.Contains(t, res.Summary(), "reasoning effort: 2 of 4 models")

	got, ok := store.Config().Providers.Get("acme")
	require.True(t, ok)
	byID := map[string]catwalk.Model{}
	for _, m := range got.Models {
		byID[m.ID] = m
	}
	require.True(t, byID["r/one"].CanReason)
	require.Equal(t, []string{"low", "medium", "high"}, byID["r/one"].ReasoningLevels)
	require.Equal(t, "medium", byID["r/one"].DefaultReasoningEffort)
	require.True(t, byID[cm.ID].CanReason, "explicit id enriched from the catalog")
	require.False(t, byID["custom-explicit"].CanReason)
	require.False(t, byID["r/plain"].CanReason)
	require.Empty(t, byID["r/plain"].ReasoningLevels)
}
