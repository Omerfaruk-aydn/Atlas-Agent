package muse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
)

// CLISession is a Muse Code CLI sign-in imported from disk, so a
// user who already ran `muse login` does not have to approve a
// second device code. Either a ready-to-use API key or an OIDC
// access token that still needs minting may be present.
type CLISession struct {
	// APIKey is a minted Model API key, usable for inference as-is.
	APIKey string
	// AccessToken is an OIDC access token. Only set when no API key
	// was stored; MintAPIKey exchanges it for one.
	AccessToken string
	// ExpiresAt is the Unix timestamp the CLI recorded for the
	// session, or zero when the file carries no expiry.
	ExpiresAt int64
}

// HasAPIKey reports whether the session can authenticate inference
// without a mint call.
func (s *CLISession) HasAPIKey() bool {
	return s != nil && s.APIKey != ""
}

// CLIAuthPath returns the Muse CLI auth file location:
// MUSE_AUTH_PATH wins when set, otherwise the XDG config home (or
// ~/.config) muse/auth.json.
func CLIAuthPath() string {
	if p := os.Getenv("MUSE_AUTH_PATH"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "muse", "auth.json")
}

// SessionFromCLI reads the official Muse CLI's stored session, if
// any. It returns nil when the CLI is not signed in (or stores its
// credentials somewhere this reader does not cover, such as the OS
// keychain), which the caller treats as "fall through to the device
// flow" rather than an error.
func SessionFromCLI() *CLISession {
	path := CLIAuthPath()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var file struct {
		Providers struct {
			Meta struct {
				Mechanism   string  `json:"mechanism"`
				AccessToken string  `json:"access_token"`
				APIKey      string  `json:"api_key"`
				ExpiresAt   float64 `json:"expires_at"`
			} `json:"meta"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil
	}
	meta := file.Providers.Meta
	if meta.Mechanism != "oauth" {
		return nil
	}
	if meta.APIKey == "" && meta.AccessToken == "" {
		return nil
	}
	return &CLISession{
		APIKey:      meta.APIKey,
		AccessToken: meta.AccessToken,
		ExpiresAt:   int64(meta.ExpiresAt),
	}
}

// TokenFromSession converts an imported CLI session into the stored
// credential shape, minting a Model API key first when the session
// only carries an OIDC access token. Imported sessions have no
// refresh token: once the key stops working the user signs in again
// with `atlas login muse`.
func TokenFromSession(ctx context.Context, session *CLISession) (*oauth.Token, error) {
	apiKey := session.APIKey
	if apiKey == "" {
		minted, err := MintAPIKey(ctx, session.AccessToken)
		if err != nil {
			return nil, err
		}
		apiKey = minted
	}
	tok := &oauth.Token{
		AccessToken: apiKey,
		ExpiresAt:   session.ExpiresAt,
	}
	tok.SetExpiresIn()
	return tok, nil
}
