package interaction

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopFailureSelectsSpecificRecovery(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"unsupported_pattern", "observation_incomplete", "focus_denied", "value_not_applied"} {
		actual, recovery := Failure(code + ": test")
		require.Equal(t, code, actual)
		require.NotEmpty(t, recovery)
	}
}
