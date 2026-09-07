package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

func newPromptRunner(t *testing.T, cmd string) *csync.Value[ptrBox[hooks.Runner]] {
	t.Helper()
	cfg := &config.Config{
		Hooks: map[string][]config.HookConfig{
			hooks.EventUserPromptSubmit: {{Command: cmd}},
		},
	}
	require.NoError(t, cfg.ValidateHooks())
	runner := hooks.NewRunner(cfg.Hooks[hooks.EventUserPromptSubmit], t.TempDir(), t.TempDir())
	return csync.NewValue(ptrBox[hooks.Runner]{v: runner})
}

func TestNoPromptHooksLeavesThePromptAlone(t *testing.T) {
	a := &sessionAgent{promptHooks: csync.NewValue(ptrBox[hooks.Runner]{})}
	got, err := a.applyPromptHooks(t.Context(), SessionAgentCall{Prompt: "ship it"})
	require.NoError(t, err)
	require.Equal(t, "ship it", got)
}

func TestPromptHookCanRefuseThePrompt(t *testing.T) {
	a := &sessionAgent{promptHooks: newPromptRunner(t, `echo '{"decision":"deny","reason":"no deploys on friday"}'`)}

	_, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "deploy"})
	require.ErrorIs(t, err, ErrPromptBlockedByHook)
	require.Contains(t, err.Error(), "no deploys on friday")
}

func TestPromptHookHaltAlsoRefuses(t *testing.T) {
	a := &sessionAgent{promptHooks: newPromptRunner(t, `echo '{"halt":true}'`)}

	_, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "deploy"})
	require.ErrorIs(t, err, ErrPromptBlockedByHook)
}

func TestPromptHookCanAddContext(t *testing.T) {
	a := &sessionAgent{promptHooks: newPromptRunner(t, `echo '{"context":"the build is red"}'`)}

	got, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "what now?"})
	require.NoError(t, err)
	require.Equal(t, "what now?\n\nthe build is red", got)
}

// A hook that says nothing must not disturb the prompt.
func TestSilentPromptHookChangesNothing(t *testing.T) {
	a := &sessionAgent{promptHooks: newPromptRunner(t, `true`)}

	got, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "hello"})
	require.NoError(t, err)
	require.Equal(t, "hello", got)
}

// The hook sees the prompt itself, which is the whole point of the event.
func TestPromptHookReceivesThePrompt(t *testing.T) {
	a := &sessionAgent{promptHooks: newPromptRunner(t,
		`grep -q "secret plan" && echo '{"decision":"deny","reason":"saw it"}'`)}

	_, err := a.applyPromptHooks(t.Context(), SessionAgentCall{SessionID: "s", Prompt: "the secret plan"})
	require.ErrorIs(t, err, ErrPromptBlockedByHook)
}

func TestAppendPromptContext(t *testing.T) {
	require.Equal(t, "p", appendPromptContext("p", ""))
	require.Equal(t, "p", appendPromptContext("p", "   "))
	require.Equal(t, "c", appendPromptContext("", "c"))
	require.Equal(t, "p\n\nc", appendPromptContext("p", "c"))
}
