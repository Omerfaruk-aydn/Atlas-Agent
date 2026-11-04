package backend

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	"github.com/stretchr/testify/require"
)

func TestElevenLabsMCPConfig_UsesOfficialPublicPKCEClient(t *testing.T) {
	t.Parallel()
	store := config.NewTestStore(&config.Config{
		MCP: config.MCPs{
			elevenLabsMCPName: {
				Type:              config.MCPHttp,
				URL:               "https://old.invalid/mcp",
				OAuthClientID:     "obsolete-client",
				EnabledTools:      []string{"text_to_speech"},
				DisabledTools:     []string{"example"},
				OAuthCallbackPort: 8765,
			},
		},
	})

	m := elevenLabsMCPConfig(store)
	require.Equal(t, config.MCPHttp, m.Type)
	require.Equal(t, elevenLabsMCPEndpoint, m.URL)
	require.True(t, m.OAuth)
	require.False(t, m.Disabled)
	require.Equal(t, elevenLabsPublicOAuthClient, m.OAuthClientID)
	require.Empty(t, m.OAuthClientSecret)
	require.Equal(t, 120, m.Timeout)
	require.Equal(t, []string{"text_to_speech"}, m.EnabledTools)
	require.Equal(t, []string{"example"}, m.DisabledTools)
	require.Equal(t, elevenLabsOAuthCallbackPort, m.OAuthCallbackPort)
	require.Equal(t, []string{
		"convai_read",
		"convai_write",
		"text_to_speech",
		"speech_history_read",
		"flows",
		"image_video_generation",
		"voice_generation",
	}, m.OAuthScopes)
	require.NotContains(t, m.OAuthScopes, "ads_read")
}

func TestApplyElevenLabsAccountState_DoesNotExposeToken(t *testing.T) {
	t.Parallel()
	views := []connections.View{{
		ID:     elevenLabsMCPName,
		Fields: map[string]string{"voice_id": "voice"},
	}}
	applyElevenLabsAccountState(views, nil)
	require.Equal(t, "true", views[0].Fields["account_oauth"])
	require.Equal(t, "voice", views[0].Fields["voice_id"])
	require.NotContains(t, views[0].Fields, "oauth_token")
}

func TestTestElevenLabsAccount_RequiresCompletedAuthorization(t *testing.T) {
	store := config.NewTestStore(&config.Config{
		MCP: config.MCPs{elevenLabsMCPName: {Type: config.MCPHttp, URL: elevenLabsMCPEndpoint, OAuth: true}},
	})
	_, handled, err := testElevenLabsAccount(store)
	require.True(t, handled)
	require.ErrorContains(t, err, "has not completed")
}
