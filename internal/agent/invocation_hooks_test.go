package agent

import (
	"os"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/stretchr/testify/require"
)

func invocationRunner(t *testing.T, event, cmd string) *hooks.Runner {
	t.Helper()
	cfg := &config.Config{Hooks: map[string][]config.HookConfig{event: {{Command: cmd}}}}
	require.NoError(t, cfg.ValidateHooks())
	return hooks.NewRunner(cfg.Hooks[event], t.TempDir(), t.TempDir())
}

func TestPreInvocationHookCanBlockAModelRequest(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{invocationHooks: csync.NewValue(ptrBox[invocationRunners]{})}
	denied, _ := a.preInvocationDenied(t.Context(), "s")
	require.False(t, denied, "no hooks configured")

	a.SetInvocationHooks(invocationRunner(t, hooks.EventPreInvocation, `echo '{"decision":"deny","reason":"budget frozen"}'`), nil, nil)
	denied, reason := a.preInvocationDenied(t.Context(), "s")
	require.True(t, denied)
	require.Equal(t, "budget frozen", reason)

	a.SetInvocationHooks(invocationRunner(t, hooks.EventPreInvocation, `exit 0`), nil, nil)
	denied, _ = a.preInvocationDenied(t.Context(), "s")
	require.False(t, denied)

	a.isSubAgent = true
	a.SetInvocationHooks(invocationRunner(t, hooks.EventPreInvocation, `echo '{"decision":"deny"}'`), nil, nil)
	denied, _ = a.preInvocationDenied(t.Context(), "s")
	require.False(t, denied, "sub-agents are not gated")
}

func TestPostInvocationHookRuns(t *testing.T) {
	t.Parallel()
	marker := t.TempDir() + "/ran"
	a := &sessionAgent{invocationHooks: csync.NewValue(ptrBox[invocationRunners]{})}
	a.SetInvocationHooks(nil, invocationRunner(t, hooks.EventPostInvocation, `echo x > "`+marker+`"`), nil)
	a.postInvocation(t.Context(), "s")
	require.FileExists(t, marker)
}

func TestAfterCompactionFiresPostCompactHooks(t *testing.T) {
	t.Parallel()
	marker := t.TempDir() + "/compacted"
	a := &sessionAgent{invocationHooks: csync.NewValue(ptrBox[invocationRunners]{})}
	a.SetInvocationHooks(nil, nil, invocationRunner(t, hooks.EventPostCompact, `echo x > "`+marker+`"`))
	a.afterCompaction(t.Context(), "s", "summary")
	require.FileExists(t, marker)

	a.isSubAgent = true
	require.NoError(t, os.Remove(marker))
	a.afterCompaction(t.Context(), "s", "summary")
	require.NoFileExists(t, marker, "sub-agents do not fire it")
}
