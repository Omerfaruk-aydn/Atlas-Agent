package vault

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceCredentialCannotRearmAnUncertainFill(t *testing.T) {
	c := NewCredentials()
	defer c.PurgeAll()
	secret := []byte("Synthetic-Source-Vault-82!")
	ready, err := c.SupplyFromSource("source-test", "turn:source", "https://login.example.test", "fixture", secret)
	require.NoError(t, err)
	require.True(t, ready)
	clear(secret)
	spec := CredentialSpec{SessionID: "source-test", Origin: "https://login.example.test", Account: "fixture", Target: CredentialTarget{TabID: "tab", Document: "doc", Selector: "#pw"}}
	req, _, err := c.Request(spec)
	require.NoError(t, err)
	_, err = c.Use(req.ID, spec.SessionID, spec.Origin, spec.Account, func(value string) (bool, error) {
		require.Equal(t, "Synthetic-Source-Vault-82!", value)
		return true, errors.New("synthetic lost acknowledgement")
	})
	require.Error(t, err)
	ready, err = c.SupplyFromSource(spec.SessionID, "turn:source", spec.Origin, spec.Account, []byte("Synthetic-Source-Vault-82!"))
	require.NoError(t, err)
	require.False(t, ready)
	require.False(t, c.ChatHasCredential(spec.SessionID))
	c.PurgeSession(spec.SessionID)
	require.Empty(t, c.sources)
}
