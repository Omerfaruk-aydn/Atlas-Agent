package vault

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRepeatedSecretRegistrationDoesNotConsumeBudget(t *testing.T) {
	t.Parallel()
	session := uuid.NewString()
	t.Cleanup(func() {
		redaction.Lock()
		delete(redaction.values, session)
		redaction.Unlock()
	})
	for range 256 {
		require.NoError(t, Register(session, "repeated-fixture-secret"))
	}
	for i := range 127 {
		require.NoError(t, Register(session, fmt.Sprintf("unique-fixture-secret-%d", i)))
	}
	require.NoError(t, Register(session, "repeated-fixture-secret"))
	require.Error(t, Register(session, "over-budget-fixture-secret"))
	require.Equal(t, "[REDACTED]", Redact(session, "repeated-fixture-secret"))
}
