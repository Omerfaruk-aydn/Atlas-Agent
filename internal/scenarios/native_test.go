package scenarios

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeScenarioRejectsAmbiguousIdentityAndUnsupportedInput(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"desktop", "android"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			s := Scenario{ID: "save", Version: 1, Target: target, Assertions: []Assertion{{Kind: "value", Selector: "id:editor", Expected: "saved"}}}
			if target == "desktop" {
				s.WindowID = "123"
			} else {
				s.Serial, s.Package = "emulator-5554", "com.example.app"
				s.Assertions[0].Selector = "com.example.app:id/editor"
			}
			data, err := json.Marshal(s)
			require.NoError(t, err)
			_, err = Parse(t.Context(), data)
			require.NoError(t, err)
			invalid := s
			invalid.URL = "https://example.test"
			require.Error(t, Validate(t.Context(), invalid))
			invalid = s
			invalid.Steps = []Step{{Action: "type", Selector: s.Assertions[0].Selector}}
			require.Error(t, Validate(t.Context(), invalid))
			invalid = s
			invalid.Steps = []Step{{Action: "resize", Width: 1280, Height: 720}}
			require.Error(t, Validate(t.Context(), invalid))
			invalid = s
			invalid.Assertions = []Assertion{{Kind: "text", Selector: s.Assertions[0].Selector}}
			require.Error(t, Validate(t.Context(), invalid))
			invalid = s
			invalid.WindowID, invalid.Serial = "", ""
			require.Error(t, Validate(t.Context(), invalid))
		})
	}
}

func TestNativeScenarioRequiresRenderedEvidence(t *testing.T) {
	t.Parallel()
	s := Scenario{ID: "save", Version: 1, Target: "android", Serial: "emulator-5554", Package: "com.example.app", Assertions: []Assertion{{Kind: "visible", Selector: "com.example.app:id/editor"}}}
	// A plain backend cannot claim native proof without a source-bound
	// observation adapter, just like web and terminal scenarios.
	run, err := Run(t.Context(), s, nil)
	require.NoError(t, err)
	require.False(t, run.Passed)
	require.Equal(t, "unavailable", run.Status)
}
