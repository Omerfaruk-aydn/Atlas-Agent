package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNewSessionAgentSupportsEveryLiveReloadSetter builds a sessionAgent the
// real way (NewSessionAgent, with zero-value SessionAgentOptions) and then
// calls every Set* method added for config live-reload. Each of those
// fields is a *csync.Value/*csync.Slice that NewSessionAgent must
// initialize even when the corresponding option is unset -- a raw struct
// literal in an individual test (as elsewhere in this package) can paper
// over a forgotten initializer by only touching the fields it cares about,
// but the real constructor can't skip any of them without panicking here.
func TestNewSessionAgentSupportsEveryLiveReloadSetter(t *testing.T) {
	agent := NewSessionAgent(SessionAgentOptions{})

	require.NotPanics(t, func() {
		agent.SetSummarizeOptions(0.5, false, &Model{})
	})
	require.NotPanics(t, func() {
		n := 3
		agent.SetLimits(&n, 10, 20)
	})
	require.NotPanics(t, func() {
		agent.SetHooks(nil, nil, nil)
	})
	require.NotPanics(t, func() {
		agent.SetAdvisorOptions(&Model{}, nil, 2, "CONCERN")
	})
	require.NotPanics(t, func() {
		agent.SetEscalateOptions(&Model{}, nil, "BLOCKER")
	})
	require.NotPanics(t, func() {
		agent.SetFallbackCooldown(15 * time.Second)
	})

	// And switching every optional setting back off (nil/zero) must be
	// just as safe as turning it on.
	require.NotPanics(t, func() {
		agent.SetSummarizeOptions(0, false, nil)
		agent.SetLimits(nil, 0, 0)
		agent.SetHooks(nil, nil, nil)
		agent.SetAdvisorOptions(nil, nil, 0, "")
		agent.SetEscalateOptions(nil, nil, "")
		agent.SetFallbackCooldown(0)
	})
}
