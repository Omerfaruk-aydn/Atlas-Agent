package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mcptools "github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
)

const (
	elevenLabsMCPName     = "elevenlabs"
	elevenLabsMCPEndpoint = "https://api.elevenlabs.io/v1/mcp"

	// ElevenLabs publishes this public PKCE client in its official open-source
	// CLI (cli/elevenlabs/main.rs). It is not a user credential or secret. The
	// registered loopback callback lets desktop clients use account OAuth without
	// hosting a Client ID Metadata Document or asking users to register an app.
	elevenLabsPublicOAuthClient = "c9c18126-5718-406a-8a12-21b69e5a888d"
	elevenLabsOAuthCallbackPort = 8484
)

// elevenLabsMCPConfig returns Atlas's managed ElevenLabs account connection.
// The public PKCE client requires no Client Secret or API key. Preserve the
// persisted token and per-user tool preferences across subsequent sign-ins.
func elevenLabsMCPConfig(store *config.ConfigStore) config.MCPConfig {
	m := store.Config().MCP[elevenLabsMCPName]
	m.Type = config.MCPHttp
	m.URL = elevenLabsMCPEndpoint
	m.OAuth = true
	m.OAuthClientID = elevenLabsPublicOAuthClient
	m.OAuthClientSecret = ""
	m.OAuthCallbackPort = elevenLabsOAuthCallbackPort
	// The resource server also advertises ads_read, but ElevenLabs does not
	// grant that scope to the official public CLI client. Request only the
	// account capabilities available to this client.
	m.OAuthScopes = []string{
		"convai_read",
		"convai_write",
		"text_to_speech",
		"speech_history_read",
		"flows",
		"image_video_generation",
		"voice_generation",
	}
	m.Disabled = false
	if m.Timeout == 0 {
		m.Timeout = 120
	}
	return m
}

func (b *Backend) startElevenLabsOAuth(workspaceID string, reauth bool) (connections.OAuthStart, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return connections.OAuthStart{}, err
	}
	account := elevenLabsMCPConfig(ws.Cfg)
	// A finished sign-in is already stored. Opening the login page again
	// only happens when the user asks to sign in once more.
	if !reauth && account.OAuthToken != nil && strings.TrimSpace(account.OAuthToken.AccessToken) != "" {
		return connections.OAuthStart{}, nil
	}
	if reauth {
		account.OAuthToken = nil
	}
	if err := ws.Cfg.SetConfigField(config.ScopeGlobal, "mcp."+elevenLabsMCPName, account); err != nil {
		return connections.OAuthStart{}, fmt.Errorf("save ElevenLabs account connection: %w", err)
	}

	finish, cancel, err := mcptools.BeginAuth(ws.Cfg, elevenLabsMCPName)
	if err != nil {
		// A popup may have been closed before Atlas returned its URL. Reuse the
		// outstanding flow instead of failing and immediately closing the next
		// popup, which previously made repeated connection attempts impossible.
		if strings.Contains(err.Error(), "already has an authentication in progress") {
			return waitForElevenLabsOAuth(nil, nil)
		}
		return connections.OAuthStart{}, err
	}

	// Authorization must outlive the short HTTP request. The local callback
	// listener completes the exchange while the web client keeps polling the
	// redacted connection state.
	done := make(chan error, 1)
	go func() {
		defer cancel()
		ctx, stop := context.WithTimeout(context.Background(), 10*time.Minute)
		defer stop()
		err := finish(ctx)
		done <- err
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("ElevenLabs OAuth did not complete", "error", err)
		}
	}()
	return waitForElevenLabsOAuth(done, cancel)
}

// waitForElevenLabsOAuth waits for either a new or an existing authorization
// flow. A nil done channel reuses the existing flow; a nil cancel leaves that
// existing flow running if this particular HTTP request times out.
func waitForElevenLabsOAuth(done <-chan error, cancel context.CancelFunc) (connections.OAuthStart, error) {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if authURL := mcptools.MCPAuthURL(elevenLabsMCPName); authURL != "" {
			return connections.OAuthStart{AuthURL: authURL}, nil
		}
		if info, ok := mcptools.GetState(elevenLabsMCPName); ok && info.State == mcptools.StateConnected {
			return connections.OAuthStart{}, nil
		}
		select {
		case err := <-done:
			if err != nil {
				return connections.OAuthStart{}, fmt.Errorf("ElevenLabs authorization failed: %w", err)
			}
			return connections.OAuthStart{}, nil
		case <-deadline.C:
			if cancel != nil {
				cancel()
			}
			return connections.OAuthStart{}, errors.New("ElevenLabs authorization URL could not be created; try again")
		case <-ticker.C:
		}
	}
}

// applyElevenLabsAccountState exposes only MCP state, never its OAuth token.
func applyElevenLabsAccountState(views []connections.View, store *config.ConfigStore) {
	for i := range views {
		if views[i].ID != elevenLabsMCPName {
			continue
		}
		if views[i].Fields == nil {
			views[i].Fields = map[string]string{}
		}
		views[i].Fields["account_oauth"] = "true"
		if info, ok := mcptools.GetState(elevenLabsMCPName); ok {
			views[i].Fields["oauth_state"] = info.State.String()
			if info.State == mcptools.StateConnected {
				views[i].Configured = true
			}
			if info.State == mcptools.StateError && info.Error != nil {
				views[i].Fields["oauth_error"] = info.Error.Error()
			}
		}
		// The saved account is the sign-in, even while the speech
		// session is still starting.
		if store != nil {
			if account, ok := store.Config().MCP[elevenLabsMCPName]; ok && account.OAuthToken != nil && strings.TrimSpace(account.OAuthToken.AccessToken) != "" {
				views[i].Configured = true
				views[i].Fields["oauth_state"] = "connected"
				delete(views[i].Fields, "oauth_error")
			}
		}
	}
}

// testElevenLabsAccount verifies the authenticated MCP session when account
// OAuth is configured. The legacy REST test remains available for API-key
// users and for the phone assistant's separate realtime configuration.
func testElevenLabsAccount(store *config.ConfigStore) (connections.TestResult, bool, error) {
	if _, configured := store.Config().MCP[elevenLabsMCPName]; !configured {
		return connections.TestResult{}, false, nil
	}
	info, ok := mcptools.GetState(elevenLabsMCPName)
	if !ok || info.State != mcptools.StateConnected {
		return connections.TestResult{}, true, errors.New("ElevenLabs account authorization has not completed; connect the account first")
	}
	return connections.TestResult{
		OK:      true,
		Message: fmt.Sprintf("ElevenLabs account connected; %d MCP tools are available to Atlas.", info.Counts.Tools),
	}, true, nil
}
