package dialog

import (
	"context"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/grok"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// NewOAuthGrok opens the OAuth dialog driving a SuperGrok or X Premium
// sign-in: a device-code flow, so the dialog shows a code to confirm at
// xAI's page while it polls in the background.
func NewOAuthGrok(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthGrok{})
}

type OAuthGrok struct {
	deviceCode *grok.DeviceCode
	cancelFunc func()
}

var _ OAuthProvider = (*OAuthGrok)(nil)

func (m *OAuthGrok) name() string {
	return "Grok"
}

func (m *OAuthGrok) initiateAuth() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceCode, err := grok.RequestDeviceCode(ctx)
	if err != nil {
		return ActionOAuthErrored{Error: fmt.Errorf("failed to initiate device auth: %w", err)}
	}

	m.deviceCode = deviceCode

	return ActionInitiateOAuth{
		DeviceCode:      deviceCode.DeviceCode,
		UserCode:        deviceCode.UserCode,
		VerificationURL: deviceCode.VerificationURL(),
		ExpiresIn:       deviceCode.ExpiresIn,
		Interval:        deviceCode.Interval,
	}
}

func (m *OAuthGrok) startPolling(_ string, _ int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel

		token, err := grok.PollForToken(ctx, m.deviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}

		return ActionCompleteOAuth{Token: token}
	}
}

func (m *OAuthGrok) stopPolling() tea.Msg {
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	return nil
}
