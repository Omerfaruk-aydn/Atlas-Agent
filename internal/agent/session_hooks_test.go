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

// A hook that was active must stop firing once a live config change turns
// it off -- not just start firing once one is added. Exercises the same
// SetHooks path in the other direction from TestSetHooksUpdatesAllThreeEventsLive.
func TestSetHooksCanTurnAnActiveHookOff(t *testing.T) {
	a := &sessionAgent{
		promptHooks:       newPromptRunner(t, `echo '{"decision":"deny","reason":"no deploys on friday"}'`),
		sessionStartHooks: noHooks(),
		preCompactHooks:   noHooks(),
		startedSessions:   csync.NewMap[string, bool](),
	}

	// Confirm the hook is actually active before turning it off.
	_, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "deploy"})
	require.ErrorIs(t, err, ErrPromptBlockedByHook)

	a.SetHooks(nil, nil, nil)

	got, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "deploy"})
	require.NoError(t, err)
	require.Equal(t, "deploy", got, "the prompt must go through unchanged once the hook is removed live")
}

// Mirrors TestSetHooksCanTurnAnActiveHookOff for the other two hook
// events: a live config change must be able to remove an active
// SessionStart or PreCompact hook, not just add one.
func TestSetHooksCanTurnSessionStartAndPreCompactOff(t *testing.T) {
	a := &sessionAgent{
		promptHooks:       noHooks(),
		sessionStartHooks: newSessionEventRunner(t, hooks.EventSessionStart, `echo '{"context":"branch: main"}'`),
		preCompactHooks:   newSessionEventRunner(t, hooks.EventPreCompact, `echo '{"decision":"deny","reason":"keep it"}'`),
		startedSessions:   csync.NewMap[string, bool](),
	}
	require.Contains(t, a.fireSessionStart(t.Context(), "s", "hi"), "branch: main")
	require.True(t, a.preCompactDenied(t.Context(), "s"))

	a.SetHooks(nil, nil, nil)

	require.Equal(t, "hi", a.fireSessionStart(t.Context(), "s2", "hi"), "a fresh session must not see the removed SessionStart hook")
	require.False(t, a.preCompactDenied(t.Context(), "s"), "PreCompact must no longer be denied once the hook is removed live")
}

func stopAgent(t *testing.T, cmd string, sub bool) *sessionAgent {
	t.Helper()
	event := hooks.EventStop
	if sub {
		event = hooks.EventSubagentStop
	}
	return &sessionAgent{stopHooks: newSessionEventRunner(t, event, cmd), isSubAgent: sub}
}

func TestStopHooksWithNoHooksLeaveTheTurnAlone(t *testing.T) {
	a := &sessionAgent{stopHooks: noHooks()}
	got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.False(t, stopped)
	require.Equal(t, evaluatorDecision{}, got)
}

func TestStopHookDenyTurnsReasonIntoTheNextInstruction(t *testing.T) {
	a := stopAgent(t, `echo '{"decision":"deny","reason":"run the linter first"}'`, false)
	got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.True(t, stopped)
	require.Equal(t, "run the linter first", got.Continue)
	require.True(t, got.stopHook)
}

func TestStopHookDenyWithoutReasonStillContinues(t *testing.T) {
	a := stopAgent(t, `echo '{"decision":"deny"}'`, false)
	got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.True(t, stopped)
	require.Equal(t, stopHookDefaultReason, got.Continue)
}

// A hook that always objects must not hold a turn open for ever.
func TestStopHookContinuationsAreCapped(t *testing.T) {
	a := stopAgent(t, `echo '{"decision":"deny","reason":"again"}'`, false)
	call := SessionAgentCall{SessionID: "s", stopHookRounds: hooks.MaxStopContinuations}
	got, stopped := a.applyStopHooks(t.Context(), call, "done", evaluatorDecision{})
	require.False(t, stopped)
	require.Empty(t, got.Continue)
}

// A gate that already refused the turn is final: the hook neither runs nor
// can undo it, and a hook can never turn an unfinished turn into a finished one.
func TestStopHookNeverOverridesAGateVerdict(t *testing.T) {
	a := stopAgent(t, `echo '{"decision":"deny","reason":"hook"}'`, false)
	for _, gate := range []evaluatorDecision{
		{Continue: "fix the failing test"},
		{Unresolved: "findings remain"},
		{Unverified: "review did not run"},
	} {
		got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", gate)
		require.False(t, stopped)
		require.Equal(t, gate, got)
	}
}

func TestStopHookAllowAndSilenceEndTheTurn(t *testing.T) {
	for _, cmd := range []string{`echo '{"decision":"allow"}'`, `true`} {
		a := stopAgent(t, cmd, false)
		got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
		require.False(t, stopped, cmd)
		require.Empty(t, got.Continue, cmd)
	}
}

// The turn is ending anyway, so a halt has nothing left to halt.
func TestStopHookHaltIsIgnored(t *testing.T) {
	a := stopAgent(t, `echo '{"halt":true,"reason":"stop"}'`, false)
	_, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.False(t, stopped)
}

func TestStopHookSeesLastMessageAndActiveFlag(t *testing.T) {
	// The script echoes back what it read from the payload on stdin.
	script := `payload=$(cat); case "$payload" in *'"stop_hook_active":true'*'"last_assistant_message":"all done"'*|*'"last_assistant_message":"all done"'*'"stop_hook_active":true'*) echo '{"decision":"deny","reason":"saw both"}';; esac`
	a := stopAgent(t, script, false)
	first, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "all done", evaluatorDecision{})
	require.False(t, stopped, "first stop is not an active continuation: %v", first)

	got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s", stopHookRounds: 1}, "all done", evaluatorDecision{})
	require.True(t, stopped)
	require.Equal(t, "saw both", got.Continue)
}

func TestSubagentStopUsesItsOwnEvent(t *testing.T) {
	a := stopAgent(t, `echo '{"decision":"deny","reason":"sub says continue"}'`, true)
	got, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.True(t, stopped)
	require.Equal(t, "sub says continue", got.Continue)
}

func TestSetStopHooksUpdatesLive(t *testing.T) {
	a := &sessionAgent{stopHooks: noHooks()}
	_, stopped := a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.False(t, stopped)

	a.SetStopHooks(newSessionEventRunner(t, hooks.EventStop, `echo '{"decision":"deny","reason":"now on"}'`).Get().v)
	_, stopped = a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.True(t, stopped)

	a.SetStopHooks(nil)
	_, stopped = a.applyStopHooks(t.Context(), SessionAgentCall{SessionID: "s"}, "done", evaluatorDecision{})
	require.False(t, stopped)
}
