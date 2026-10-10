package dialog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/antigravity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// NewOAuthAntigravity opens the OAuth dialog driving an Antigravity
// (Google AI Pro/Ultra) sign-in: a PKCE + localhost redirect flow like
// ChatGPT's, plus a Cloud project discovery/provisioning step once the
// token is in hand.
func NewOAuthAntigravity(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthAntigravity{})
}

type OAuthAntigravity struct {
	mu         sync.Mutex
	session    antigravityLoginSession
	cancelFunc func()
	status     string
	stopped    bool
}

type antigravityLoginSession interface {
	WaitWithProgress(context.Context, func(string)) (*oauth.Token, error)
	Close()
}

var _ OAuthProvider = (*OAuthAntigravity)(nil)

func (m *OAuthAntigravity) name() string {
	return "Antigravity"
}

func (m *OAuthAntigravity) initiateAuth() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := antigravity.Start(ctx)
	if err != nil {
		return ActionOAuthErrored{Error: fmt.Errorf("failed to start Antigravity sign-in: %w", err)}
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		session.Close()
		return nil
	}
	m.session = session
	m.mu.Unlock()

	return ActionInitiateOAuth{
		DeviceCode:      "",
		UserCode:        "",
		VerificationURL: session.AuthURL(),
		// Project discovery/provisioning after the redirect can take a
		// little while for a brand-new account, so give this more room
		// than ChatGPT's window.
		ExpiresIn: 15 * 60,
		Interval:  0,
	}
}

func (m *OAuthAntigravity) startPolling(_ string, expiresIn int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(expiresIn)*time.Second)
		defer cancel()
		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			return nil
		}
		m.cancelFunc = cancel
		session := m.session
		m.mu.Unlock()

		token, err := session.WaitWithProgress(ctx, func(status string) {
			m.mu.Lock()
			m.status = status
			m.mu.Unlock()
		})
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}

		return ActionCompleteOAuth{Token: token}
	}
}

func (m *OAuthAntigravity) stopPolling() tea.Msg {
	m.mu.Lock()
	m.stopped = true
	cancel, session := m.cancelFunc, m.session
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if session != nil {
		session.Close()
	}
	return nil
}

func (m *OAuthAntigravity) progressStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}
