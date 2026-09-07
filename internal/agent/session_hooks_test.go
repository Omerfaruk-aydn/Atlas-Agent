package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

func newSessionEventRunner(t *testing.T, event, cmd string) *csync.Value[ptrBox[hooks.Runner]] {
	t.Helper()
	cfg := &config.Config{
		Hooks: map[string][]config.HookConfig{
			event: {{Command: cmd}},
		},
	}
	require.NoError(t, cfg.ValidateHooks())
	runner := hooks.NewRunner(cfg.Hooks[event], t.TempDir(), t.TempDir())
	return csync.NewValue(ptrBox[hooks.Runner]{v: runner})
}

func noHooks() *csync.Value[ptrBox[hooks.Runner]] {
	return csync.NewValue(ptrBox[hooks.Runner]{})
}

func TestFireSessionStartWithNoHooksLeavesThePromptAlone(t *testing.T) {
	a := &sessionAgent{sessionStartHooks: noHooks(), startedSessions: csync.NewMap[string, bool]()}
	got := a.fireSessionStart(t.Context(), "s", "hello")
	require.Equal(t, "hello", got)
}

func TestFireSessionStartAddsContextAheadOfThePrompt(t *testing.T) {
	a := &sessionAgent{
		sessionStartHooks: newSessionEventRunner(t, hooks.EventSessionStart, `echo '{"context":"branch: main"}'`),
		startedSessions:   csync.NewMap[string, bool](),
	}
	got := a.fireSessionStart(t.Context(), "s", "what's up?")
	require.Equal(t, "branch: main\n\nwhat's up?", got)
}

func TestFireSessionStartFiresOnlyOncePerSession(t *testing.T) {
	a := &sessionAgent{
		sessionStartHooks: newSessionEventRunner(t, hooks.EventSessionStart, `echo '{"context":"once"}'`),
		startedSessions:   csync.NewMap[string, bool](),
	}
	first := a.fireSessionStart(t.Context(), "s", "first prompt")
	require.Contains(t, first, "once")

	second := a.fireSessionStart(t.Context(), "s", "second prompt")
	require.Equal(t, "second prompt", second, "the hook must not fire again for a session it already started")
}

func TestFireSessionStartFiresSeparatelyPerSession(t *testing.T) {
	a := &sessionAgent{
		sessionStartHooks: newSessionEventRunner(t, hooks.EventSessionStart, `echo '{"context":"hi"}'`),
		startedSessions:   csync.NewMap[string, bool](),
	}
	require.Contains(t, a.fireSessionStart(t.Context(), "session-a", "p"), "hi")
	require.Contains(t, a.fireSessionStart(t.Context(), "session-b", "p"), "hi", "a different session must still get its own start")
}

func TestFireSessionStartSilentHookChangesNothing(t *testing.T) {
	a := &sessionAgent{
		sessionStartHooks: newSessionEventRunner(t, hooks.EventSessionStart, `true`),
		startedSessions:   csync.NewMap[string, bool](),
	}
	require.Equal(t, "p", a.fireSessionStart(t.Context(), "s", "p"))
}

func TestPreCompactDeniedWithNoHooksAllowsCompaction(t *testing.T) {
	a := &sessionAgent{preCompactHooks: noHooks()}
	require.False(t, a.preCompactDenied(t.Context(), "s"))
}

func TestPreCompactDeniedOnDenyDecision(t *testing.T) {
	a := &sessionAgent{preCompactHooks: newSessionEventRunner(t, hooks.EventPreCompact, `echo '{"decision":"deny","reason":"keep it all"}'`)}
	require.True(t, a.preCompactDenied(t.Context(), "s"))
}

func TestPreCompactDeniedOnHalt(t *testing.T) {
	a := &sessionAgent{preCompactHooks: newSessionEventRunner(t, hooks.EventPreCompact, `echo '{"halt":true}'`)}
	require.True(t, a.preCompactDenied(t.Context(), "s"))
}

func TestPreCompactAllowsCompactionWhenTheHookSaysNothing(t *testing.T) {
	a := &sessionAgent{preCompactHooks: newSessionEventRunner(t, hooks.EventPreCompact, `true`)}
	require.False(t, a.preCompactDenied(t.Context(), "s"))
}

// A live config change to any of the three hook events must reach an
// already-running session -- see SetHooks.
func TestSetHooksUpdatesAllThreeEventsLive(t *testing.T) {
	a := &sessionAgent{
		promptHooks:       noHooks(),
		sessionStartHooks: noHooks(),
		preCompactHooks:   noHooks(),
		startedSessions:   csync.NewMap[string, bool](),
	}

	prompt := newPromptRunner(t, `echo '{"context":"prompt hook"}'`).Get().v
	start := newSessionEventRunner(t, hooks.EventSessionStart, `echo '{"context":"start hook"}'`).Get().v
	preCompact := newSessionEventRunner(t, hooks.EventPreCompact, `echo '{"decision":"deny","reason":"keep it"}'`).Get().v

	a.SetHooks(prompt, start, preCompact)

	got, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "hi"})
	require.NoError(t, err)
	require.Contains(t, got, "prompt hook")

	require.Contains(t, a.fireSessionStart(t.Context(), "s2", "hi"), "start hook")

	require.True(t, a.preCompactDenied(t.Context(), "s"))
}
