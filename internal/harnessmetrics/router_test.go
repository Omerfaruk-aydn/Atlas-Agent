package harnessmetrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRouterSavings(t *testing.T) {
	t.Parallel()
	events := []Event{
		{Kind: RouterStep, Session: "a", Attrs: map[string]string{"saved": "0.25"}},
		{Kind: RouterStep, Session: "a", Attrs: map[string]string{"saved": "bad"}},
		{Kind: RouterSwitch, Session: "a"},
		{Kind: RouterStep, Session: "b", Attrs: map[string]string{"saved": "1"}},
	}
	a := RouterSavings(events, "a")
	require.Equal(t, 2, a.RoutedSteps)
	require.Equal(t, 1, a.Switches)
	require.InDelta(t, 0.25, a.Saved, 1e-9)
	require.InDelta(t, 1.25, RouterSavings(events, "").Saved, 1e-9)
	require.Contains(t, FormatRouterSavings(a), "router saved ~$0.2500")
	require.Empty(t, FormatRouterSavings(RouterTotals{}))
	require.Contains(t, Format(Summarize(events), time.Now()), "Model router")
}
