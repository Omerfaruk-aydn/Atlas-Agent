package dialog

import (
	"context"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/minimax"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// NewOAuthMiniMax opens the OAuth dialog driving a MiniMax Token Plan
// subscription sign-in. Like Kimi's, this is a device-code flow: the
// dialog shows a code to confirm at the verification URL while it polls
// MiniMax in the background. The region follows the selected provider
// (minimax-plan is global, minimax-plan-cn is China).
func NewOAuthMiniMax(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	region := minimax.Global
	if provider.ID == catwalk.InferenceProviderMiniMaxPlanCN {
		region = minimax.China
	}
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthMiniMax{region: region})
}

type OAuthMiniMax struct {
	region     minimax.Region
	deviceCode *minimax.DeviceCode
	cancelFunc func()
}

var _ OAuthProvider = (*OAuthMiniMax)(nil)

func (m *OAuthMiniMax) name() string {
	if m.region == minimax.China {
		return "MiniMax Token Plan (China)"
	}
	return "MiniMax Token Plan"
}

func (m *OAuthMiniMax) initiateAuth() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceCode, err := minimax.RequestDeviceCode(ctx, m.region)
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

func (m *OAuthMiniMax) startPolling(deviceCode string, expiresIn int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel

		token, err := minimax.PollForToken(ctx, m.region, m.deviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}

		return ActionCompleteOAuth{Token: token}
	}
}

func (m *OAuthMiniMax) stopPolling() tea.Msg {
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	return nil
}
