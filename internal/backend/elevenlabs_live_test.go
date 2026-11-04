package backend

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestElevenLabsLiveOAuthDiscovery exercises the real provider through client
// discovery and authorization-URL generation. It deliberately stops before
// account sign-in; no credentials or tokens are used or persisted.
func TestElevenLabsLiveOAuthDiscovery(t *testing.T) {
	if os.Getenv("ATLAS_LIVE_OAUTH_TEST") != "1" {
		t.Skip("set ATLAS_LIVE_OAUTH_TEST=1 to exercise the real ElevenLabs OAuth discovery")
	}
	b, ws, _ := newPublishingWorkspace(t)
	result, err := b.startElevenLabsOAuth(ws.ID, false)
	require.NoError(t, err)
	require.NotEmpty(t, result.AuthURL)

	u, err := url.Parse(result.AuthURL)
	require.NoError(t, err)
	require.Equal(t, "https", u.Scheme)
	require.Equal(t, "elevenlabs.io", u.Host)
	require.Equal(t, "/app/oauth/authorize", u.Path)
	require.Equal(t, elevenLabsPublicOAuthClient, u.Query().Get("client_id"))
	require.Equal(t, "http://localhost:8484/callback", u.Query().Get("redirect_uri"))
	require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
	require.NotEmpty(t, u.Query().Get("code_challenge"))
	require.NotEmpty(t, u.Query().Get("state"))
	scopes := strings.Fields(u.Query().Get("scope"))
	require.ElementsMatch(t, []string{
		"convai_read",
		"convai_write",
		"text_to_speech",
		"speech_history_read",
		"flows",
		"image_video_generation",
		"voice_generation",
	}, scopes)
	require.NotContains(t, scopes, "ads_read")

	// A retry while the browser authorization is outstanding must return the
	// existing URL rather than reporting "authentication in progress".
	retry, err := b.startElevenLabsOAuth(ws.ID)
	require.NoError(t, err)
	require.Equal(t, result.AuthURL, retry.AuthURL)
}
