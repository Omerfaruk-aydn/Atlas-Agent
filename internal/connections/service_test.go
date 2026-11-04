package connections

import (
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/stretchr/testify/require"
)

func TestListRedactsSecretValues(t *testing.T) {
	t.Parallel()
	const secret = "must-never-leave-the-server"
	cfg := &config.Config{}
	cfg.Tools.VoiceCalls = config.ToolVoiceCalls(voice.Config{
		Owner:      voice.OwnerConfig{PhoneNumber: "+905551112233"},
		ElevenLabs: voice.ElevenLabsConfig{APIKey: voice.SecretRef(secret), VoiceID: "voice"},
		Twilio:     voice.TwilioConfig{AccountSID: voice.SecretRef(secret), AuthToken: voice.SecretRef(secret)},
		Telegram:   voice.TelegramConfig{BotToken: voice.SecretRef(secret)},
	})
	views := New(config.NewTestStore(cfg), t.TempDir()).List()
	data, err := json.Marshal(views)
	require.NoError(t, err)
	require.NotContains(t, string(data), secret)
	require.True(t, views[0].Secrets["api_key"])
	require.True(t, views[3].Available)
	require.False(t, views[3].Configured)
	for _, view := range views {
		require.True(t, view.Available)
	}
}

func TestDecodeSaveRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	_, err := DecodeSave([]byte(`{"api_key":"value","unexpected":"not-allowed"}`))
	require.Error(t, err)
	req, err := DecodeSave([]byte(`{"voice_id":"voice","voice_enabled":true}`))
	require.NoError(t, err)
	require.Equal(t, "voice", req.VoiceID)
	require.True(t, *req.VoiceEnabled)
}

func TestSaveValidatesPhoneAndPublicURL(t *testing.T) {
	t.Parallel()
	svc := New(config.NewTestStore(&config.Config{}), t.TempDir())
	err := svc.Save("twilio", SaveRequest{PhoneNumber: "0555", OwnerNumber: "+905551112233", PublicBaseURL: "https://example.com"})
	require.ErrorContains(t, err, "E.164")
	err = svc.Save("twilio", SaveRequest{PhoneNumber: "+905551112233", OwnerNumber: "+905551112233", PublicBaseURL: "http://example.com"})
	require.ErrorContains(t, err, "HTTPS")
	require.Error(t, svc.Save("google", SaveRequest{}))
}
