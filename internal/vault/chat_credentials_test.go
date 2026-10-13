package vault

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChatCredentialBindsOnceWithoutPhone(t *testing.T) {
	c := NewCredentials()
	t.Cleanup(func() { c.PurgeAll() })
	secret := "Synthetic-Chat-77!"
	prompt, err := c.CaptureUserPrompt("chat-one", "gmail hesabına user@example.test ile giriş yap şifre de bu "+secret)
	require.NoError(t, err)
	require.NotContains(t, prompt, secret)
	require.Contains(t, prompt, "credential_request")
	// Sanitized prompts pass through both coordinator and agent unchanged.
	again, err := c.CaptureUserPrompt("chat-one", prompt)
	require.NoError(t, err)
	require.Equal(t, prompt, again)
	spec := CredentialSpec{SessionID: "chat-one", Origin: "https://accounts.google.com", Account: "user@example.test", Target: CredentialTarget{TabID: "tab", Document: "doc", Selector: "#pw", NodeToken: "node"}}
	wrong := spec
	wrong.SessionID = "other"
	_, _, err = c.Request(wrong)
	require.ErrorIs(t, err, ErrNoInputChannel)
	wrong = spec
	wrong.Origin = "https://phishing.example.test"
	_, _, err = c.Request(wrong)
	require.ErrorIs(t, err, ErrNoInputChannel)
	wrong = spec
	wrong.Account = "other@example.test"
	_, _, err = c.Request(wrong)
	require.ErrorIs(t, err, ErrNoInputChannel)
	req, reused, err := c.Request(spec)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, CredentialProvided, req.State)
	same, reused, err := c.Request(spec)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, req.ID, same.ID)
	require.False(t, c.ChatHasCredential("chat-one"))
	_, err = c.Use(req.ID, spec.SessionID, spec.Origin, spec.Account, func(value string) (bool, error) {
		require.Equal(t, secret, value)
		return true, nil
	})
	require.NoError(t, err)
	_, err = c.Use(req.ID, spec.SessionID, spec.Origin, spec.Account, func(string) (bool, error) {
		t.Fatal("must not resend")
		return true, nil
	})
	require.Error(t, err)
}

func TestChatCredentialParsingAndLifecycle(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		capture     bool
	}{
		{"quoted", `https://login.example.test/ giriş yap user@example.test password: "two words!"`, true},
		{"turkish", "gmail user@example.test şifrem Synth-22!", true},
		{"natural possessive", "gmail i açıp user@example.test hesabını ekle bu da şifresi Synth-22!", true},
		{"possessive password", "https://login.example.test user@example.test parolası Synth-22!", true},
		{"missing password", "gmail user@example.test şifresi yok", false},
		{"wrong password description", "gmail user@example.test şifresi yanlış", false},
		{"explicit literal", `gmail user@example.test şifresi "wrong"`, true},
		{"not a password", "gmail user@example.test şifre alanına git", false},
		{"multiple accounts", "gmail a@example.test b@example.test password: Synth-22!", false},
		{"multiple services", "gmail outlook user@example.test password: Synth-22!", false},
		{"code", "gmail user@example.test ```password: Synth-22!```", false},
		{"missing account", "gmail password: Synth-22!", false},
		{"password is not a provider hint", "https://login.example.test/ user@example.test password: gmail-outlook!", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCredentials()
			defer c.PurgeAll()
			out, err := c.CaptureUserPrompt(tt.name, tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.capture, c.ChatHasCredential(tt.name))
			if !tt.capture {
				require.Equal(t, tt.input, out)
			}
		})
	}
	c := NewCredentials()
	defer c.PurgeAll()
	_, err := c.CaptureUserPrompt("expiry", "gmail user@example.test password: Synth-22!")
	require.NoError(t, err)
	e := c.chat["expiry"]
	c.PurgeSession("expiry")
	require.Empty(t, e.secret)
	require.False(t, c.ChatHasCredential("expiry"))
	_, err = c.CaptureUserPrompt("expiry", "gmail user@example.test password: Synth-22!")
	require.NoError(t, err)
	c.now = func() time.Time { return time.Now().Add(CredentialRequestTTL + time.Second) }
	_, _, err = c.Request(CredentialSpec{SessionID: "expiry", Origin: "https://accounts.google.com", Account: "user@example.test"})
	require.ErrorIs(t, err, ErrNoInputChannel)
	require.Empty(t, c.chat)
}
