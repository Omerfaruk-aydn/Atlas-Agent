// Package accountlogin signs in to a subscription account without a terminal,
// for the desktop app. It runs the same flows as `atlas-agent login` and ends
// with the same token; only the prompting is left to the caller.
package accountlogin

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/claude"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/codex"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/copilot"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/grok"
	kimioauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/kimi"
	minimaxoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/minimax"
	museoauth "github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/muse"
)

// providers are the catalog ids that sign in with an account.
var providers = []string{
	"copilot", "chatgpt", "antigravity", "claude", "muse", "kimi-code", "minimax-plan", "minimax-plan-cn", "grok-web",
}

// Supported reports whether a provider signs in with an account.
func Supported(provider string) bool {
	return slices.Contains(providers, provider)
}

// Session is a sign-in in progress.
type Session struct {
	// URL is the page the person signs in on. It is empty when an existing
	// login on this machine was taken over and nothing is left to do.
	URL string
	// UserCode is what the person types on that page. It is empty for flows
	// that come back to a local callback instead.
	UserCode string

	wait  func(ctx context.Context, progress func(string)) (*oauth.Token, error)
	close func()
}

// NewSession builds a session from its parts, for callers that stand in for a
// real provider.
func NewSession(url, userCode string, wait func(context.Context, func(string)) (*oauth.Token, error)) *Session {
	return &Session{URL: url, UserCode: userCode, wait: wait}
}

// Wait blocks until the person finishes signing in. Progress, when the flow
// has any, is reported in plain text.
func (s *Session) Wait(ctx context.Context, progress func(string)) (*oauth.Token, error) {
	return s.wait(ctx, progress)
}

// Close releases what the session holds, such as a local callback port.
func (s *Session) Close() {
	if s.close != nil {
		s.close()
	}
}

// Start begins signing in to provider. ctx bounds the whole sign-in, not only
// this call.
func Start(ctx context.Context, provider string) (*Session, error) {
	switch provider {
	case "copilot":
		return startCopilot(ctx)
	case "chatgpt":
		s, err := codex.Start(ctx)
		if err != nil {
			return nil, err
		}
		return &Session{
			URL:   s.AuthURL(),
			wait:  func(ctx context.Context, _ func(string)) (*oauth.Token, error) { return s.Wait(ctx) },
			close: s.Close,
		}, nil
	case "antigravity":
		s, err := antigravity.Start(ctx)
		if err != nil {
			return nil, err
		}
		return &Session{URL: s.AuthURL(), wait: s.WaitWithProgress, close: s.Close}, nil
	case "claude":
		return startClaude(ctx)
	case "muse":
		return startMuse(ctx)
	case "kimi-code":
		dc, err := kimioauth.RequestDeviceCode(ctx)
		if err != nil {
			return nil, err
		}
		return &Session{
			URL:      dc.VerificationURL(),
			UserCode: dc.UserCode,
			wait: func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
				return kimioauth.PollForToken(ctx, dc)
			},
		}, nil
	case "minimax-plan", "minimax-plan-cn":
		return startMiniMax(ctx, provider)
	case "grok-web":
		dc, err := grok.RequestDeviceCode(ctx)
		if err != nil {
			return nil, err
		}
		return &Session{
			URL:      dc.VerificationURL(),
			UserCode: dc.UserCode,
			wait: func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
				return grok.PollForToken(ctx, dc)
			},
		}, nil
	}
	return nil, fmt.Errorf("%s does not sign in with an account", provider)
}

// taken is a session for a login this machine already has.
func taken(token *oauth.Token) *Session {
	return &Session{wait: func(context.Context, func(string)) (*oauth.Token, error) { return token, nil }}
}

func startCopilot(ctx context.Context) (*Session, error) {
	if disk, ok := copilot.RefreshTokenFromDisk(); ok {
		token, err := copilot.RefreshToken(ctx, disk)
		if err == nil {
			return taken(token), nil
		}
	}
	dc, err := copilot.RequestDeviceCode(ctx)
	if err != nil {
		return nil, err
	}
	return &Session{
		URL:      dc.VerificationURI,
		UserCode: dc.UserCode,
		wait: func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
			token, err := copilot.PollForToken(ctx, dc)
			if errors.Is(err, copilot.ErrNotAvailable) {
				return nil, fmt.Errorf("GitHub Copilot is not available for this account; sign up at %s", copilot.SignupURL)
			}
			return token, err
		},
	}, nil
}

// startClaude takes over a Claude Code login when the machine has one, as
// the terminal does: the browser's consent screen refuses other callers.
func startClaude(ctx context.Context) (*Session, error) {
	if _, err := claude.DescribeExisting(); err == nil {
		token, err := claude.ImportExisting()
		if err != nil {
			return nil, fmt.Errorf("import the Claude Code login: %w", err)
		}
		return taken(token), nil
	}
	s, err := claude.Start(ctx)
	if err != nil {
		return nil, err
	}
	return &Session{URL: s.AuthURL(), wait: s.WaitWithProgress, close: s.Close}, nil
}

func startMuse(ctx context.Context) (*Session, error) {
	if cli := museoauth.SessionFromCLI(); cli != nil {
		token, err := museoauth.TokenFromSession(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("use the Muse CLI session: %w", err)
		}
		return taken(token), nil
	}
	dc, err := museoauth.RequestDeviceCode(ctx)
	if err != nil {
		return nil, err
	}
	return &Session{
		URL:      dc.VerificationURL(),
		UserCode: dc.UserCode,
		wait: func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
			return museoauth.PollForToken(ctx, dc)
		},
	}, nil
}

func startMiniMax(ctx context.Context, provider string) (*Session, error) {
	region := minimaxoauth.Global
	if provider == "minimax-plan-cn" {
		region = minimaxoauth.China
	}
	dc, err := minimaxoauth.RequestDeviceCode(ctx, region)
	if err != nil {
		return nil, err
	}
	return &Session{
		URL:      dc.VerificationURL(),
		UserCode: dc.UserCode,
		wait: func(ctx context.Context, _ func(string)) (*oauth.Token, error) {
			return minimaxoauth.PollForToken(ctx, region, dc)
		},
	}, nil
}
