package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActivityOverlayConfiguration(t *testing.T) {
	var c ToolComputer
	var b ToolBrowser
	require.NoError(t, json.Unmarshal([]byte(`{"overlay":false,"overlay_reduced_motion":true}`), &c))
	require.NoError(t, json.Unmarshal([]byte(`{"overlay":false,"overlay_reduced_motion":true,"headless":false}`), &b))
	require.False(t, *c.Overlay)
	require.False(t, *b.Overlay)
	require.True(t, c.OverlayReducedMotion)
	require.True(t, b.OverlayReducedMotion)
	require.False(t, b.IsHeadless())
	require.Nil(t, ToolComputer{}.Overlay)
	require.Nil(t, ToolBrowser{}.Overlay)
}
