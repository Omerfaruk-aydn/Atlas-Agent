package dialog

import (
	"context"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth/muse"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// NewOAuthMuse opens the OAuth dialog driving a Meta Muse Code
// subscription sign-in. Like Copilot's, this is a device-code flow:
// the dialog shows a code to type at the verification URL while it
// polls Meta in the background.
//
// Unlike `atlas login muse`, this dialog always runs the device flow
// and never imports an existing Muse CLI credential: the CLI keeps
// that shortcut, while picking a Muse model in the TUI is an explicit
// request to authenticate.
func NewOAuthMuse(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthMuse{})
}

type OAuthMuse struct {
	deviceCode *muse.DeviceCode
	cancelFunc func()
}

var _ OAuthProvider = (*OAuthMuse)(nil)

func (m *OAuthMuse) name() string {
	return "Muse"
}

func (m *OAuthMuse) initiateAuth() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceCode, err := muse.RequestDeviceCode(ctx)
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

func (m *OAuthMuse) startPolling(deviceCode string, expiresIn int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel

		token, err := muse.PollForToken(ctx, m.deviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}

		return ActionCompleteOAuth{Token: token}
	}
}

func (m *OAuthMuse) stopPolling() tea.Msg {
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	return nil
}
