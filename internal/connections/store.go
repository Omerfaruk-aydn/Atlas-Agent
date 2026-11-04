// Package connections manages user-configured service credentials without
// putting plaintext secrets in Atlas configuration or API responses.
package connections

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

const (
	ElevenLabsAPIKey = "ATLAS_CONNECTION_ELEVENLABS_API_KEY"
	MailboxAddress   = "mailbox-address"
	MailboxSecret    = "mailbox-secret"
	TwilioAccountSID = "ATLAS_CONNECTION_TWILIO_ACCOUNT_SID"
	TwilioAuthToken  = "ATLAS_CONNECTION_TWILIO_AUTH_TOKEN"
	TelegramBotToken = "ATLAS_CONNECTION_TELEGRAM_BOT_TOKEN"
	googleClientID   = "google-client-id"
	googleSecret     = "google-client-secret"
	googleToken      = "google-oauth-token"
	slackToken       = "slack-bot-token"
	discordToken     = "discord-bot-token"
	discordClientID  = "discord-client-id"
	discordSecret    = "discord-client-secret"
	githubToken      = "github-token"
	notionToken      = "notion-token"
	outlookClientID  = "outlook-client-id"
	outlookSecret    = "outlook-client-secret"
	outlookToken     = "outlook-oauth-token"
	slackClientID    = "slack-client-id"
	slackSecret      = "slack-client-secret"
	githubClientID   = "github-client-id"
	githubSecret     = "github-client-secret"
	notionClientID   = "notion-client-id"
	notionSecret     = "notion-client-secret"
)

var secretNames = map[string]string{
	ElevenLabsAPIKey: "elevenlabs-api-key",
	MailboxAddress:   "mailbox-address",
	MailboxSecret:    "mailbox-secret",
	TwilioAccountSID: "twilio-account-sid",
	TwilioAuthToken:  "twilio-auth-token",
	TelegramBotToken: "telegram-bot-token",
	googleClientID:   "google-client-id",
	googleSecret:     "google-client-secret",
	googleToken:      "google-oauth-token",
	slackToken:       "slack-bot-token",
	discordToken:     "discord-bot-token",
	discordClientID:  "discord-client-id",
	discordSecret:    "discord-client-secret",
	githubToken:      "github-token",
	notionToken:      "notion-token",
	outlookClientID:  "outlook-client-id",
	outlookSecret:    "outlook-client-secret",
	outlookToken:     "outlook-oauth-token",
	slackClientID:    "slack-client-id",
	slackSecret:      "slack-client-secret",
	githubClientID:   "github-client-id",
	githubSecret:     "github-client-secret",
	notionClientID:   "notion-client-id",
	notionSecret:     "notion-client-secret",
}

// SecretStore persists credentials with the same OS protection as the vault.
type SecretStore struct{ Dir string }

func (s SecretStore) path(name string) (string, error) {
	file, ok := secretNames[name]
	if !ok {
		return "", errors.New("unknown connection credential")
	}
	return filepath.Join(s.Dir, file+".secret"), nil
}

// Set encrypts and atomically replaces one credential.
func (s SecretStore) Set(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 {
		return errors.New("credential must contain 1-4096 characters")
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	sealed, err := vault.Seal([]byte(value))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Get decrypts a credential. It never returns an environment fallback.
func (s SecretStore) Get(name string) (string, error) {
	path, err := s.path(name)
	if err != nil {
		return "", err
	}
	sealed, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	plain, err := vault.Unseal(sealed)
	if err != nil {
		return "", errors.New("stored connection credential is unavailable")
	}
	defer clear(plain)
	return string(plain), nil
}

// Env resolves connection credentials first, then the process environment.
// A missing stored value falls back to the environment for existing installs.
func Env(dataDir string) func(string) string {
	store := SecretStore{Dir: filepath.Join(dataDir, "connections", "secrets")}
	return func(name string) string {
		if _, ok := secretNames[name]; ok {
			if value, err := store.Get(name); err == nil && value != "" {
				return value
			}
		}
		return os.Getenv(name)
	}
}
