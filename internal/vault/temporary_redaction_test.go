package vault

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTemporaryCredentialDoesNotRetainPlaintextInRedaction(t *testing.T) {
	session := uuid.NewString()
	secret := "Synth-🙂-Quoted\"\\Secret"
	c, _, _ := newTestCredentials(t)
	req, _, err := c.Request(spec(session))
	require.NoError(t, err)
	_, err = c.Provide(req.ID, session, "phone", []byte(secret), false)
	require.NoError(t, err)
	_, err = c.Use(req.ID, session, req.Origin, req.Account, func(got string) (bool, error) {
		require.Equal(t, secret, got)
		return true, nil
	})
	require.NoError(t, err)
	redaction.RLock()
	require.NotContains(t, redaction.values[session], secret)
	redaction.RUnlock()
	c.mu.Lock()
	require.Empty(t, c.entries[req.ID].secret)
	c.mu.Unlock()
	encoded, _ := json.Marshal(secret)
	require.Equal(t, "[REDACTED]", Redact(session, secret))
	require.Equal(t, `"[REDACTED]"`, Redact(session, string(encoded)))
}

func TestCredentialExpiryWithoutFurtherStoreCalls(t *testing.T) {
	c, _, _ := newTestCredentials(t)
	req, _, err := c.Request(spec(uuid.NewString()))
	require.NoError(t, err)
	_, err = c.Provide(req.ID, req.SessionID, "phone", []byte(syntheticSecret), false)
	require.NoError(t, err)
	// Exercise the actual expiry callback without waiting two minutes.
	c.mu.Lock()
	c.scheduleExpiryLocked(c.entries[req.ID], 15*time.Millisecond)
	c.mu.Unlock()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.entries[req.ID].req.State == CredentialExpired && len(c.entries[req.ID].secret) == 0
	}, time.Second, 5*time.Millisecond)
}
