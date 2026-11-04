package connections

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mailbox"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// View is safe to return to a browser: it contains no secret or secret value.
type View struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Kind        string            `json:"kind"`
	Configured  bool              `json:"configured"`
	Fields      map[string]string `json:"fields,omitempty"`
	Secrets     map[string]bool   `json:"secrets,omitempty"`
	Available   bool              `json:"available"`
}

// SaveRequest is the allowlisted browser-editable connection configuration.
type SaveRequest struct {
	APIKey        string `json:"api_key,omitempty"`
	VoiceID       string `json:"voice_id,omitempty"`
	STTModel      string `json:"stt_model,omitempty"`
	TTSModel      string `json:"tts_model,omitempty"`
	AccountSID    string `json:"account_sid,omitempty"`
	AuthToken     string `json:"auth_token,omitempty"`
	PhoneNumber   string `json:"phone_number,omitempty"`
	OwnerNumber   string `json:"owner_number,omitempty"`
	WhatsAppFrom  string `json:"whatsapp_from,omitempty"`
	PublicBaseURL string `json:"public_base_url,omitempty"`
	BotToken      string `json:"bot_token,omitempty"`
	ChatID        string `json:"chat_id,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
	Token         string `json:"token,omitempty"`
	ChannelID     string `json:"channel_id,omitempty"`
	Repository    string `json:"repository,omitempty"`
	MailAddress   string `json:"mail_address,omitempty"`
	MailSecret    string `json:"mail_secret,omitempty"`
	Tenant        string `json:"tenant,omitempty"`
	VoiceEnabled  *bool  `json:"voice_enabled,omitempty"`
}

// TestResult reports a real read-only provider request.
type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Service saves and tests the supported connections for one config store.
type Service struct {
	Store                *config.ConfigStore
	DataDir              string
	HTTP                 *http.Client
	GoogleEndpoint       oauth2.Endpoint
	GoogleUserInfoURL    string
	ProviderTestURLs     map[string]string
	ProviderAPIURLs      map[string]string
	OutlookEndpoint      oauth2.Endpoint
	OutlookUserURL       string
	SocialOAuthEndpoints map[string]oauth2.Endpoint
	oauthMu              sync.Mutex
	oauthPending         map[string]pendingGoogleOAuth
}

func New(store *config.ConfigStore, dataDir string) *Service {
	return &Service{Store: store, DataDir: dataDir, HTTP: &http.Client{Timeout: 12 * time.Second}}
}

func (s *Service) secrets() SecretStore {
	return SecretStore{Dir: filepath.Join(s.DataDir, "connections", "secrets")}
}

func ref(name string) voice.SecretRef { return voice.SecretRef("$" + name) }

// List returns stable catalog entries, including future extension points.
func (s *Service) List() []View {
	cfg := voice.Config(s.Store.Config().Tools.VoiceCalls).WithDefaults()
	googleID, _ := s.secrets().Get(googleClientID)
	googleClientSecret, _ := s.secrets().Get(googleSecret)
	googleOAuthToken, _ := s.secrets().Get(googleToken)
	slackBotToken, _ := s.secrets().Get(slackToken)
	discordBotToken, _ := s.secrets().Get(discordToken)
	githubAccessToken, _ := s.secrets().Get(githubToken)
	notionIntegrationToken, _ := s.secrets().Get(notionToken)
	mailboxAddr, _ := s.secrets().Get(MailboxAddress)
	mailboxSecret, _ := s.secrets().Get(MailboxSecret)
	outlookID, _ := s.secrets().Get(outlookClientID)
	outlookClientSecret, _ := s.secrets().Get(outlookSecret)
	outlookOAuthToken, _ := s.secrets().Get(outlookToken)
	socialSecrets := func(id string) map[string]bool {
		provider := socialOAuth[id]
		clientID, _ := s.secrets().Get(provider.clientID)
		clientSecret, _ := s.secrets().Get(provider.secret)
		return map[string]bool{"client_id": clientID != "", "client_secret": clientSecret != ""}
	}
	views := []View{
		{
			ID: "elevenlabs", Name: "ElevenLabs", Kind: "speech", Available: true,
			Description: "Connect your ElevenLabs account with OAuth for agent speech tools, or configure the separate phone assistant with an API key.",
			Configured:  cfg.ElevenLabs.APIKey != "" && cfg.ElevenLabs.VoiceID != "",
			Fields:      map[string]string{"voice_id": cfg.ElevenLabs.VoiceID, "stt_model": cfg.ElevenLabs.STTModel, "tts_model": cfg.ElevenLabs.TTSModel},
			Secrets:     map[string]bool{"api_key": cfg.ElevenLabs.APIKey != ""},
		},
		{
			ID: "twilio", Name: "Telefon ve WhatsApp", Kind: "telephony", Available: true,
			Description: "Twilio phone calls, SMS and owner-only WhatsApp fallback messages.",
			Configured:  cfg.Twilio.AccountSID != "" && cfg.Twilio.AuthToken != "" && cfg.Twilio.PhoneNumber != "",
			Fields:      map[string]string{"phone_number": cfg.Twilio.PhoneNumber, "owner_number": cfg.Owner.PhoneNumber, "whatsapp_from": cfg.Twilio.WhatsAppFrom, "public_base_url": cfg.Twilio.PublicBaseURL, "voice_enabled": fmt.Sprint(cfg.Enabled)},
			Secrets:     map[string]bool{"account_sid": cfg.Twilio.AccountSID != "", "auth_token": cfg.Twilio.AuthToken != ""},
		},
		{
			ID: "telegram", Name: "Telegram", Kind: "messaging", Available: true,
			Description: "Written fallback notifications through a Telegram bot.",
			Configured:  cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "",
			Fields:      map[string]string{"chat_id": cfg.Telegram.ChatID},
			Secrets:     map[string]bool{"bot_token": cfg.Telegram.BotToken != ""},
		},
		{
			ID: "google", Name: "Google Workspace ve YouTube", Kind: "productivity", Available: true,
			Description: "Google OAuth for Gmail, Calendar, Drive, Docs, Sheets and YouTube video management.",
			Configured:  googleOAuthToken != "",
			Secrets:     map[string]bool{"client_id": googleID != "", "client_secret": googleClientSecret != ""},
		},
		{ID: "slack", Name: "Slack", Kind: "messaging", Available: true, Description: "Slack account OAuth, channel reading and messaging.", Configured: slackBotToken != "", Secrets: socialSecrets("slack")},
		{ID: "discord", Name: "Discord", Kind: "messaging", Available: true, Description: "Discord bot authentication and identity verification.", Configured: discordBotToken != "", Secrets: map[string]bool{"token": discordBotToken != ""}},
		{ID: "github", Name: "GitHub", Kind: "developer", Available: true, Description: "GitHub account OAuth for repository, issue and pull-request workflows.", Configured: githubAccessToken != "", Secrets: socialSecrets("github")},
		{ID: "notion", Name: "Notion", Kind: "productivity", Available: true, Description: "Notion account OAuth for pages and databases.", Configured: notionIntegrationToken != "", Secrets: socialSecrets("notion")},
		{ID: "outlook", Name: "Outlook ve Microsoft Calendar", Kind: "productivity", Available: true, Description: "Microsoft OAuth authorization for Outlook mail and Calendar.", Configured: outlookOAuthToken != "", Secrets: map[string]bool{"client_id": outlookID != "", "client_secret": outlookClientSecret != ""}},
		{ID: "mailbox", Name: "Posta", Kind: "mail", Available: true, Description: "Saved mailbox used to read sign-in codes.", Configured: mailboxAddr != "" && mailboxSecret != "", Fields: map[string]string{"address": mailboxAddr}, Secrets: map[string]bool{"mail_secret": mailboxSecret != ""}},
	}
	return views
}

// Save validates and persists only fields belonging to id. Empty secrets keep
// their existing value; configuration always stores references, never values.
func (s *Service) Save(id string, req SaveRequest) error {
	fields := map[string]any{}
	secretValues := map[string]string{}
	trim := func(v string) string { return strings.TrimSpace(v) }
	switch id {
	case "elevenlabs":
		key := trim(req.APIKey)
		voiceID := trim(req.VoiceID)
		if key == "" && voiceID == "" {
			return errors.New("ElevenLabs API key is required")
		}
		if key != "" {
			secretValues[ElevenLabsAPIKey] = key
			fields["tools.voice_calls.provider"] = voice.DefaultProvider
			fields["tools.voice_calls.elevenlabs.api_key"] = ref(ElevenLabsAPIKey)
		}
		if voiceID != "" {
			fields["tools.voice_calls.provider"] = voice.DefaultProvider
			fields["tools.voice_calls.elevenlabs.voice_id"] = voiceID
			fields["tools.voice_calls.elevenlabs.stt_model"] = trim(req.STTModel)
			fields["tools.voice_calls.elevenlabs.tts_model"] = trim(req.TTSModel)
		}
	case "twilio":
		if req.PhoneNumber != "" && !voice.ValidE164(trim(req.PhoneNumber)) || !voice.ValidE164(trim(req.OwnerNumber)) {
			return errors.New("Twilio and owner phone numbers must use E.164 format, for example +905551112233")
		}
		if req.WhatsAppFrom != "" && !voice.ValidE164(trim(req.WhatsAppFrom)) {
			return errors.New("WhatsApp sender must use E.164 format without the whatsapp: prefix")
		}
		u, err := url.Parse(trim(req.PublicBaseURL))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("public callback URL must be a valid HTTPS URL")
		}
		secretValues[TwilioAccountSID] = trim(req.AccountSID)
		secretValues[TwilioAuthToken] = trim(req.AuthToken)
		fields["tools.voice_calls.transport"] = voice.DefaultTransport
		fields["tools.voice_calls.owner.phone_number"] = trim(req.OwnerNumber)
		fields["tools.voice_calls.twilio.phone_number"] = trim(req.PhoneNumber)
		fields["tools.voice_calls.twilio.whatsapp_from"] = trim(req.WhatsAppFrom)
		fields["tools.voice_calls.twilio.public_base_url"] = strings.TrimRight(trim(req.PublicBaseURL), "/")
		if req.VoiceEnabled != nil {
			fields["tools.voice_calls.enabled"] = *req.VoiceEnabled
		}
		if secretValues[TwilioAccountSID] != "" {
			fields["tools.voice_calls.twilio.account_sid"] = ref(TwilioAccountSID)
		}
		if secretValues[TwilioAuthToken] != "" {
			fields["tools.voice_calls.twilio.auth_token"] = ref(TwilioAuthToken)
		}
	case "telegram":
		if trim(req.ChatID) == "" {
			return errors.New("Telegram chat ID is required")
		}
		secretValues[TelegramBotToken] = trim(req.BotToken)
		fields["tools.voice_calls.telegram.chat_id"] = trim(req.ChatID)
		if secretValues[TelegramBotToken] != "" {
			fields["tools.voice_calls.telegram.bot_token"] = ref(TelegramBotToken)
		}
	case "google":
		existingID, _ := s.secrets().Get(googleClientID)
		if trim(req.ClientID) == "" && existingID == "" {
			return errors.New("Google OAuth client ID is required")
		}
		if req.ClientID != "" && !validGoogleClientID(req.ClientID) {
			return errors.New("Google OAuth client ID is invalid")
		}
		secretValues[googleClientID] = trim(req.ClientID)
		secretValues[googleSecret] = trim(req.ClientSecret)
	case "slack", "github", "notion":
		provider := socialOAuth[id]
		existingID, _ := s.secrets().Get(provider.clientID)
		if trim(req.ClientID) == "" && existingID == "" && trim(req.Token) == "" {
			return errors.New("OAuth client ID is required")
		}
		if req.ClientID != "" && !validGoogleClientID(req.ClientID) {
			return errors.New("OAuth client ID is invalid")
		}
		secretValues[provider.clientID] = trim(req.ClientID)
		secretValues[provider.secret] = trim(req.ClientSecret)
		secretValues[provider.token] = trim(req.Token)
	case "discord":
		secretValues[discordToken] = trim(req.Token)
	case "mailbox":
		address := trim(req.MailAddress)
		secret := trim(req.MailSecret)
		if !strings.Contains(address, "@") || strings.ContainsAny(address, " \t\r\n") {
			return errors.New("geçerli bir e-posta adresi yaz")
		}
		if secret == "" || strings.ContainsAny(secret, "\r\n") {
			return errors.New("posta şifresi gerekli")
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := mailbox.Check(checkCtx, address, secret); err != nil {
			return err
		}
		secretValues[MailboxAddress] = address
		secretValues[MailboxSecret] = secret
	case "outlook":
		existingID, _ := s.secrets().Get(outlookClientID)
		if trim(req.ClientID) == "" && existingID == "" {
			return errors.New("Microsoft application client ID is required")
		}
		if req.ClientID != "" && !validGoogleClientID(req.ClientID) {
			return errors.New("Microsoft application client ID is invalid")
		}
		secretValues[outlookClientID] = trim(req.ClientID)
		secretValues[outlookSecret] = trim(req.ClientSecret)
	default:
		return errors.New("this connection is not configurable yet")
	}
	for name, value := range secretValues {
		if value != "" {
			if err := s.secrets().Set(name, value); err != nil {
				return fmt.Errorf("save encrypted credential: %w", err)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return s.Store.SetConfigFields(config.ScopeGlobal, fields)
}

// Test performs a cheap read-only request. It never places a call or sends a message.
func (s *Service) Test(ctx context.Context, id string) (TestResult, error) {
	if _, ok := tokenConnectionSecrets[id]; ok {
		if err := s.testTokenConnection(ctx, id); err != nil {
			return TestResult{}, err
		}
		return TestResult{OK: true, Message: "Connection verified with a read-only provider request."}, nil
	}
	if id == "outlook" {
		if err := s.testOutlook(ctx); err != nil {
			return TestResult{}, err
		}
		return TestResult{OK: true, Message: "Microsoft account verified with a read-only identity request."}, nil
	}
	if id == "google" {
		if err := s.testGoogle(ctx); err != nil {
			return TestResult{}, err
		}
		return TestResult{OK: true, Message: "Google account verified with a read-only identity request."}, nil
	}
	env := Env(s.DataDir)
	cfg := voice.Config(s.Store.Config().Tools.VoiceCalls)
	var req *http.Request
	var err error
	switch id {
	case "elevenlabs":
		key, resolveErr := cfg.ElevenLabs.APIKey.Resolve(env)
		if resolveErr != nil {
			return TestResult{}, errors.New("save an ElevenLabs API key first")
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.elevenlabs.io/v1/user", nil)
		if err == nil {
			req.Header.Set("xi-api-key", key.Reveal())
		}
	case "twilio":
		sid, sidErr := cfg.Twilio.AccountSID.Resolve(env)
		token, tokenErr := cfg.Twilio.AuthToken.Resolve(env)
		if sidErr != nil || tokenErr != nil {
			return TestResult{}, errors.New("save the Twilio account SID and auth token first")
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.twilio.com/2010-04-01/Accounts/"+url.PathEscape(sid.Reveal())+".json", nil)
		if err == nil {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(sid.Reveal()+":"+token.Reveal())))
		}
	case "telegram":
		token, resolveErr := cfg.Telegram.BotToken.Resolve(env)
		if resolveErr != nil {
			return TestResult{}, errors.New("save a Telegram bot token first")
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+url.PathEscape(token.Reveal())+"/getMe", nil)
	default:
		return TestResult{}, errors.New("this connection does not support testing yet")
	}
	if err != nil {
		return TestResult{}, errors.New("could not create provider request")
	}
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return TestResult{}, errors.New("provider could not be reached")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return TestResult{}, errors.New("provider rejected the credentials")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TestResult{}, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	return TestResult{OK: true, Message: "Connection verified with a read-only provider request."}, nil
}

// DecodeSave enforces the request schema at the HTTP boundary.
func DecodeSave(data []byte) (SaveRequest, error) {
	var req SaveRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return SaveRequest{}, errors.New("invalid connection settings")
	}
	return req, nil
}
