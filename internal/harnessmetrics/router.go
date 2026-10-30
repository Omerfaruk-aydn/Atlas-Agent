package harnessmetrics

import (
	"fmt"
	"strconv"
)

// Events written by the automatic model router (options.router).
const (
	// RouterStep is recorded for every step the router ran on the fast
	// model. Attrs: model, saved (estimated dollars, versus the strong
	// model at the same token counts).
	RouterStep Kind = "router_step"
	// RouterSwitch is recorded when the router changes tier. Attrs: from,
	// to, reason.
	RouterSwitch Kind = "router_switch"
)

// RouterTotals is what the router did over a set of events.
type RouterTotals struct {
	// RoutedSteps counts steps that ran on the fast model.
	RoutedSteps int
	// Switches counts tier changes.
	Switches int
	// Saved is the estimated dollars saved against running every routed
	// step on the strong model.
	Saved float64
}

// RouterSavings totals the router events of one session, or of every
// session when sessionID is empty.
func RouterSavings(events []Event, sessionID string) RouterTotals {
	var t RouterTotals
	for _, e := range events {
		if sessionID != "" && e.Session != sessionID {
			continue
		}
		switch e.Kind {
		case RouterStep:
			t.RoutedSteps++
			if saved, err := strconv.ParseFloat(e.Attrs["saved"], 64); err == nil && saved > 0 {
				t.Saved += saved
			}
		case RouterSwitch:
			t.Switches++
		}
	}
	return t
}

// FormatRouterSavings renders totals as "router saved ~$0.0123 (12 steps,
// 3 switches)", or "" when the router did nothing.
func FormatRouterSavings(t RouterTotals) string {
	if t.RoutedSteps == 0 && t.Switches == 0 {
		return ""
	}
	return fmt.Sprintf("router saved ~$%.4f (%d steps on the fast model, %d switches)", t.Saved, t.RoutedSteps, t.Switches)
}
