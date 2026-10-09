package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/stretchr/testify/require"
)

func TestOAuthRefreshSurvivesRestartWithoutReplacementRefreshToken(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"claude", "antigravity", "chatgpt"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "atlas.json")
			previous := &oauth.Token{
				AccessToken: "expired-access", RefreshToken: "renewable-refresh",
				ExpiresIn: 3600, ExpiresAt: time.Now().Add(-time.Hour).Unix(),
				AccountID: "account-or-project", PlanType: "pro", Email: "test@example.test",
				OrgID: "org", OrgName: "Test Org", IssuedAt: 1700000000,
				Client: &oauth.OAuthClient{ClientID: "client", TokenURL: "https://example.test/token"},
			}
			providers := csync.NewMap[string, ProviderConfig]()
			providers.Set(id, ProviderConfig{ID: id, APIKey: previous.AccessToken, OAuthToken: previous})
			cfg := &Config{Providers: providers}
			data, err := json.Marshal(cfg)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data, 0o600))
			exchanges := 0
			exchange := func(_ context.Context, provider, refresh string) (*oauth.Token, error) {
				require.Equal(t, id, provider)
				if refresh != previous.RefreshToken {
					return nil, fmt.Errorf("saved refresh token was lost")
				}
				exchanges++
				return &oauth.Token{AccessToken: fmt.Sprintf("access-%d", exchanges), ExpiresIn: 3600, ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
			}
			store := &ConfigStore{config: cfg, globalDataPath: path, exchangeToken: exchange}
			require.NoError(t, store.RefreshOAuthToken(t.Context(), ScopeGlobal, id))
			data, err = os.ReadFile(path)
			require.NoError(t, err)
			var restarted Config
			require.NoError(t, json.Unmarshal(data, &restarted))
			pc, ok := restarted.Providers.Get(id)
			require.True(t, ok)
			require.Equal(t, previous.RefreshToken, pc.OAuthToken.RefreshToken)
			require.Equal(t, previous.AccountID, pc.OAuthToken.AccountID)
			require.Equal(t, previous.PlanType, pc.OAuthToken.PlanType)
			require.Equal(t, previous.Email, pc.OAuthToken.Email)
			require.Equal(t, previous.OrgID, pc.OAuthToken.OrgID)
			require.Equal(t, previous.OrgName, pc.OAuthToken.OrgName)
			require.Equal(t, previous.IssuedAt, pc.OAuthToken.IssuedAt)
			require.Equal(t, previous.Client, pc.OAuthToken.Client)
			pc.OAuthToken.ExpiresAt = time.Now().Add(-time.Minute).Unix()
			restarted.Providers.Set(id, pc)
			store = &ConfigStore{config: &restarted, globalDataPath: path, exchangeToken: exchange}
			require.NoError(t, store.RefreshOAuthToken(t.Context(), ScopeGlobal, id))
			require.Equal(t, 2, exchanges)
		})
	}
}

// TestLiveStoredOAuthRenewal explicitly opts into refreshing saved credentials.
// The normal store locks and persists rotated tokens; credentials are never logged.
func TestLiveStoredOAuthRenewal(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_OAUTH_REFRESH") != "1" {
		t.Skip("Live saved-credential refresh is disabled")
	}
	path := os.Getenv("ATLAS_LIVE_OAUTH_CONFIG")
	require.NotEmpty(t, path)
	for _, id := range []string{"claude", "antigravity"} {
		t.Run(id, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("Could not read saved configuration")
			}
			var cfg Config
			if json.Unmarshal(data, &cfg) != nil {
				t.Fatal("Could not parse saved configuration")
			}
			pc, ok := cfg.Providers.Get(id)
			require.True(t, ok)
			require.NotNil(t, pc.OAuthToken)
			store := &ConfigStore{config: &cfg, globalDataPath: path}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			if err := store.RefreshOAuthToken(ctx, ScopeGlobal, id); err != nil {
				var exchangeErr *oauth.TokenExchangeError
				if errors.As(err, &exchangeErr) {
					t.Fatalf("Refresh failed: HTTP %d, revoked=%t", exchangeErr.StatusCode, exchangeErr.IsRefreshTokenRevoked())
				}
				t.Fatal("Saved-credential refresh failed")
			}
			updated, _ := store.Config().Providers.Get(id)
			require.False(t, updated.OAuthToken.IsExpired())
			require.True(t, updated.OAuthToken.RefreshToken != "", "Refresh must remain possible after restart")
		})
	}
}
