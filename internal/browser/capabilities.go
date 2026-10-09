package browser

import (
	"context"
	"time"
)

// The optional capabilities below let the tool layer use richer driver
// features while lightweight and mock drivers remain valid Sessions.

// FormSession fills several fields and selects options with readback.
type FormSession interface {
	FillForm(ctx context.Context, p Request) (FormResult, error)
	SelectOption(ctx context.Context, p Request) (FieldResult, error)
}

// CollectSession scans scrolling, virtualized and paginated lists.
type CollectSession interface {
	Collect(ctx context.Context, p Request) (CollectResult, error)
}

// ResearchSession reads independent pages in background tabs.
type ResearchSession interface {
	Research(ctx context.Context, p Request) ([]ResearchSource, error)
}

// HealthSession probes connection, tab and page state without input.
type HealthSession interface {
	Health(ctx context.Context) Health
}

// EvalSession bounds how long an evaluated promise may be awaited.
type EvalSession interface {
	EvalContext(ctx context.Context, expression string, timeout time.Duration) (string, error)
}

// TabIdentity reports the selected tab's id.
type TabIdentity interface {
	ActiveTab() string
}

// KeySession sends one normalized key or chord through the typed input
// path, so held keys are released even when dispatch fails.
type KeySession interface {
	Input(ctx context.Context, p Request) (InputResult, error)
}

var (
	_ FieldSession     = (*chromedpSession)(nil)
	_ FormSession      = (*chromedpSession)(nil)
	_ CollectSession   = (*chromedpSession)(nil)
	_ ResearchSession  = (*chromedpSession)(nil)
	_ HealthSession    = (*chromedpSession)(nil)
	_ EvalSession      = (*chromedpSession)(nil)
	_ TabIdentity      = (*chromedpSession)(nil)
	_ ObservingSession = (*chromedpSession)(nil)
	_ KeySession       = (*chromedpSession)(nil)
)
