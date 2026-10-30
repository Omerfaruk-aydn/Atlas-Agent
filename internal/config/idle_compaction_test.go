package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdleCompactionPolicyDefaultsAndOptIn(t *testing.T) {
	t.Parallel()
	var defaults *Harness
	_, _, enabled := defaults.IdleCompactionPolicy()
	require.False(t, enabled, "background model calls must be opt-in")

	h := &Harness{IdleCompactMinutes: 15}
	idle, tokens, enabled := h.IdleCompactionPolicy()
	require.True(t, enabled)
	require.Equal(t, 15*time.Minute, idle)
	require.Equal(t, int64(32_000), tokens)

	h.IdleCompactTokens = 50_000
	_, tokens, _ = h.IdleCompactionPolicy()
	require.Equal(t, int64(50_000), tokens)
}
