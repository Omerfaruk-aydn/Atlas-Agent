package voice

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Defaults. The assistant is off until the user turns it on, and every
// limit errs on the cheap side.
const (
	DefaultProvider              = "elevenlabs"
	DefaultTransport             = "twilio"
	DefaultLanguage              = "tr-TR"
	DefaultMaxCallSeconds        = 180
	DefaultMaxDailyMinutes       = 30
	DefaultMaxConcurrentCalls    = 1
	DefaultSilenceTimeoutSeconds = 20
	DefaultTurnTimeoutSeconds    = 90
	DefaultMaxToolCallsPerTurn   = 8
	DefaultMaxOutboundPerHour    = 3
	DefaultMinOutboundInterval   = 600
	DefaultRingTimeoutSeconds    = 30
	DefaultPublicAddr            = "127.0.0.1:8787"
	DefaultControlAddr           = "127.0.0.1:8788"

	maxCallSecondsLimit  = 3600
	maxDailyMinutesLimit = 24 * 60
)

// Config is the provider-neutral voice assistant configuration. It lives
// under tools.voice_calls in atlas.json. Credentials are never written
// here directly: every credential field holds a [SecretRef].
type Config struct {
	Enabled bool `json:"enabled,omitempty" jsonschema:"description=Turn on the phone voice assistant. Off by default."`
	// Provider is the default speech provider for both directions.
	Provider string `json:"provider,omitempty" jsonschema:"description=Speech provider for recognition and synthesis,default=elevenlabs"`
	// Recognizer and Synthesizer override Provider per direction.
	Recognizer  string `json:"stt_provider,omitempty" jsonschema:"description=Speech-to-text provider override"`
	Synthesizer string `json:"tts_provider,omitempty" jsonschema:"description=Text-to-speech provider override"`
	Transport   string `json:"transport,omitempty" jsonschema:"description=Call transport: twilio or whatsapp,default=twilio"`
	Language    string `json:"language,omitempty" jsonschema:"description=BCP-47 conversation language,default=tr-TR"`

	MaxCallSeconds        int  `json:"max_call_seconds,omitempty" jsonschema:"description=Longest allowed call,default=180"`
	MaxDailyMinutes       int  `json:"max_daily_minutes,omitempty" jsonschema:"description=Call minutes allowed per day across all calls,default=30"`
	MaxConcurrentCalls    int  `json:"max_concurrent_calls,omitempty" jsonschema:"description=Simultaneous calls,default=1"`
	SilenceTimeoutSeconds int  `json:"silence_timeout_seconds,omitempty" jsonschema:"description=Hang up after this long without speech,default=20"`
	TurnTimeoutSeconds    int  `json:"turn_timeout_seconds,omitempty" jsonschema:"description=Longest one agent answer may take,default=90"`
	MaxToolCallsPerTurn   int  `json:"max_tool_calls_per_turn,omitempty" jsonschema:"description=Tool calls one spoken request may make,default=8"`
	RecordCalls           bool `json:"record_calls,omitempty" jsonschema:"description=Keep call audio. Not supported; must stay false."`
	// StoreTranscripts defaults to true; transcripts stay on this machine.
	StoreTranscripts *bool `json:"store_transcripts,omitempty" jsonschema:"description=Keep call transcripts on this machine,default=true"`

	Owner      OwnerConfig      `json:"owner,omitzero"`
	Outbound   OutboundConfig   `json:"outbound,omitzero"`
	Listen     ListenConfig     `json:"listen,omitzero"`
	ElevenLabs ElevenLabsConfig `json:"elevenlabs,omitzero"`
	Twilio     TwilioConfig     `json:"twilio,omitzero"`
	WhatsApp   WhatsAppConfig   `json:"whatsapp,omitzero"`
	Telegram   TelegramConfig   `json:"telegram,omitzero"`
	ToolPolicy ToolPolicyConfig `json:"tool_policy,omitzero"`
	Monitor    MonitorConfig    `json:"monitor,omitzero"`
}

// OwnerConfig is the only person the assistant talks to.
type OwnerConfig struct {
	// PhoneNumber is the E.164 number outbound calls go to. It may be a
	// SecretRef such as $ATLAS_OWNER_PHONE.
	PhoneNumber string `json:"phone_number,omitempty" jsonschema:"description=Owner's E.164 phone number or an environment reference like $ATLAS_OWNER_PHONE"`
	// AllowedCallers may call in. The owner number is always allowed.
	AllowedCallers []string `json:"allowed_callers,omitempty" jsonschema:"description=Extra E.164 numbers allowed to call in"`
	// Timezone is used for spoken dates and per-day budgets.
	Timezone string `json:"timezone,omitempty" jsonschema:"description=IANA timezone for dates and daily limits,example=Europe/Istanbul"`
}

// OutboundConfig limits calls the assistant places.
type OutboundConfig struct {
	MaxPerHour         int  `json:"max_per_hour,omitempty" jsonschema:"description=Outbound calls per rolling hour,default=3"`
	MinIntervalSeconds int  `json:"min_interval_seconds,omitempty" jsonschema:"description=Minimum gap between outbound calls,default=600"`
	RingTimeoutSeconds int  `json:"ring_timeout_seconds,omitempty" jsonschema:"description=How long to ring before giving up,default=30"`
	AllowRetry         bool `json:"allow_retry,omitempty" jsonschema:"description=Allow one controlled retry of an unanswered urgent call"`
	// Fallback is the ordered list of written channels tried after an
	// unanswered call: sms, whatsapp, telegram.
	Fallback []string `json:"fallback,omitempty" jsonschema:"description=Written fallback channels after an unanswered call: sms, whatsapp, telegram"`
}

// ListenConfig sets where the voice service listens.
type ListenConfig struct {
	// PublicAddr serves Twilio webhooks and media streams. Put it behind a
	// TLS-terminating proxy or tunnel; PublicBaseURL must be https.
	PublicAddr string `json:"public_addr,omitempty" jsonschema:"description=Address for Twilio webhooks behind a TLS proxy,default=127.0.0.1:8787"`
	// ControlAddr serves the internal API agents use. Loopback only.
	ControlAddr string `json:"control_addr,omitempty" jsonschema:"description=Loopback address of the internal voice API,default=127.0.0.1:8788"`
}

// ElevenLabsConfig configures the ElevenLabs speech provider.
type ElevenLabsConfig struct {
	APIKey   SecretRef `json:"api_key,omitempty" jsonschema:"description=Environment reference to the API key,example=$ELEVENLABS_API_KEY"`
	STTModel string    `json:"stt_model,omitempty" jsonschema:"description=Realtime recognition model,default=scribe_v2_realtime"`
	TTSModel string    `json:"tts_model,omitempty" jsonschema:"description=Synthesis model,default=eleven_flash_v2_5"`
	VoiceID  string    `json:"voice_id,omitempty" jsonschema:"description=Voice used for replies"`
	Keyterms []string  `json:"keyterms,omitempty" jsonschema:"description=Names and terms recognition should favour"`
	// BaseURL and StreamURL override the endpoints, for tests and proxies.
	BaseURL   string `json:"base_url,omitempty" jsonschema:"-"`
	StreamURL string `json:"stream_url,omitempty" jsonschema:"-"`
}

// TwilioConfig configures the Twilio Programmable Voice transport.
type TwilioConfig struct {
	AccountSID  SecretRef `json:"account_sid,omitempty" jsonschema:"description=Environment reference to the account SID,example=$TWILIO_ACCOUNT_SID"`
	AuthToken   SecretRef `json:"auth_token,omitempty" jsonschema:"description=Environment reference to the auth token,example=$TWILIO_AUTH_TOKEN"`
	PhoneNumber string    `json:"phone_number,omitempty" jsonschema:"description=Twilio number in E.164"`
	// WhatsAppFrom is the Twilio WhatsApp sender used for written
	// fallback, without the whatsapp: prefix.
	WhatsAppFrom string `json:"whatsapp_from,omitempty" jsonschema:"description=Twilio WhatsApp sender for written fallback"`
	// PublicBaseURL is the https URL Twilio reaches PublicAddr through.
	PublicBaseURL    string `json:"public_base_url,omitempty" jsonschema:"description=Public https URL that reaches public_addr"`
	MachineDetection bool   `json:"machine_detection,omitempty" jsonschema:"description=Ask Twilio to detect answering machines on outbound calls"`
	// APIBaseURL overrides the REST endpoint, for tests.
	APIBaseURL string `json:"api_base_url,omitempty" jsonschema:"-"`
}

// DefaultWhatsAppGraphVersion is the Graph API version used when none is
// set.
const DefaultWhatsAppGraphVersion = "v23.0"

// WhatsAppConfig configures the WhatsApp Business Calling transport. Calls
// travel over WebRTC; the Graph API and webhooks only carry signalling.
type WhatsAppConfig struct {
	AccessToken SecretRef `json:"access_token,omitempty" jsonschema:"description=Environment reference to the Cloud API access token,example=$WHATSAPP_ACCESS_TOKEN"`
	AppSecret   SecretRef `json:"app_secret,omitempty" jsonschema:"description=Environment reference to the Meta app secret that signs webhooks,example=$WHATSAPP_APP_SECRET"`
	VerifyToken SecretRef `json:"verify_token,omitempty" jsonschema:"description=Environment reference to the token Meta sends when verifying the webhook,example=$WHATSAPP_VERIFY_TOKEN"`
	// PhoneNumberID is the Cloud API id of the business number, not the
	// phone number itself.
	PhoneNumberID string `json:"phone_number_id,omitempty" jsonschema:"description=Cloud API phone number id of the business number"`
	GraphVersion  string `json:"graph_version,omitempty" jsonschema:"description=Graph API version,default=v23.0"`
	// STUNServers help the media path cross NAT. Peers see this machine's
	// public address through them.
	STUNServers []string `json:"stun_servers,omitempty" jsonschema:"description=STUN servers for the media path,default=stun:stun.l.google.com:19302"`
	// APIBaseURL overrides the Graph endpoint, for tests.
	APIBaseURL string `json:"api_base_url,omitempty" jsonschema:"-"`
}

// TelegramConfig configures the Telegram written fallback.
type TelegramConfig struct {
	BotToken SecretRef `json:"bot_token,omitempty" jsonschema:"description=Environment reference to the bot token"`
	ChatID   string    `json:"chat_id,omitempty" jsonschema:"description=Chat the bot writes to"`
}

// ToolPolicyConfig adjusts which agent tools a voice turn may use.
type ToolPolicyConfig struct {
	// Allow runs without spoken confirmation (read-only tools, such as an
	// MCP mail search). Glob patterns.
	Allow []string `json:"allow,omitempty" jsonschema:"description=Tools a voice turn may run without spoken confirmation (glob)"`
	// Block is never run from a call. Glob patterns, added to defaults.
	Block []string `json:"block,omitempty" jsonschema:"description=Tools never run from a call (glob), in addition to the defaults"`
	// AllowEmailSend lifts the default block on sending mail. Sending
	// still needs spoken confirmation.
	AllowEmailSend bool `json:"allow_email_send,omitempty" jsonschema:"description=Allow sending email from a call after spoken confirmation"`
}

// MonitorConfig lists sites the voice service watches.
type MonitorConfig struct {
	Sites          []SiteConfig `json:"sites,omitempty" jsonschema:"description=Sites checked on a schedule; the owner is called on state changes"`
	MaxConcurrency int          `json:"max_concurrency,omitempty" jsonschema:"description=Parallel checks,default=2"`
}

// SiteConfig is one monitored URL.
type SiteConfig struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	IntervalSeconds  int    `json:"interval_seconds,omitempty" jsonschema:"default=60"`
	TimeoutSeconds   int    `json:"timeout_seconds,omitempty" jsonschema:"default=10"`
	FailureThreshold int    `json:"failure_threshold,omitempty" jsonschema:"default=2"`
	// DownStatuses are HTTP statuses counted as failures in addition to
	// 5xx, such as 403.
	DownStatuses []int `json:"down_statuses,omitempty"`
	// NotifyOnRecovery calls again when the site comes back.
	NotifyOnRecovery bool   `json:"notify_on_recovery,omitempty"`
	Priority         string `json:"priority,omitempty" jsonschema:"enum=normal,enum=urgent,default=urgent"`
}

// WithDefaults returns c with every unset field at its default.
func (c Config) WithDefaults() Config {
	c.Provider = cmp.Or(c.Provider, DefaultProvider)
	c.Transport = cmp.Or(c.Transport, DefaultTransport)
	c.Language = cmp.Or(c.Language, DefaultLanguage)
	c.MaxCallSeconds = cmp.Or(c.MaxCallSeconds, DefaultMaxCallSeconds)
	c.MaxDailyMinutes = cmp.Or(c.MaxDailyMinutes, DefaultMaxDailyMinutes)
	c.MaxConcurrentCalls = cmp.Or(c.MaxConcurrentCalls, DefaultMaxConcurrentCalls)
	c.SilenceTimeoutSeconds = cmp.Or(c.SilenceTimeoutSeconds, DefaultSilenceTimeoutSeconds)
	c.TurnTimeoutSeconds = cmp.Or(c.TurnTimeoutSeconds, DefaultTurnTimeoutSeconds)
	c.MaxToolCallsPerTurn = cmp.Or(c.MaxToolCallsPerTurn, DefaultMaxToolCallsPerTurn)
	c.Outbound.MaxPerHour = cmp.Or(c.Outbound.MaxPerHour, DefaultMaxOutboundPerHour)
	c.Outbound.MinIntervalSeconds = cmp.Or(c.Outbound.MinIntervalSeconds, DefaultMinOutboundInterval)
	c.Outbound.RingTimeoutSeconds = cmp.Or(c.Outbound.RingTimeoutSeconds, DefaultRingTimeoutSeconds)
	c.Listen.PublicAddr = cmp.Or(c.Listen.PublicAddr, DefaultPublicAddr)
	c.Listen.ControlAddr = cmp.Or(c.Listen.ControlAddr, DefaultControlAddr)
	c.WhatsApp.GraphVersion = cmp.Or(c.WhatsApp.GraphVersion, DefaultWhatsAppGraphVersion)
	if len(c.WhatsApp.STUNServers) == 0 {
		c.WhatsApp.STUNServers = []string{"stun:stun.l.google.com:19302"}
	}
	c.ElevenLabs.STTModel = cmp.Or(c.ElevenLabs.STTModel, "scribe_v2_realtime")
	c.ElevenLabs.TTSModel = cmp.Or(c.ElevenLabs.TTSModel, "eleven_flash_v2_5")
	c.Monitor.MaxConcurrency = cmp.Or(c.Monitor.MaxConcurrency, 2)
	for i := range c.Monitor.Sites {
		s := &c.Monitor.Sites[i]
		s.IntervalSeconds = cmp.Or(s.IntervalSeconds, 60)
		s.TimeoutSeconds = cmp.Or(s.TimeoutSeconds, 10)
		s.FailureThreshold = cmp.Or(s.FailureThreshold, 2)
		s.Priority = cmp.Or(s.Priority, PriorityUrgent)
	}
	return c
}

// STTProvider is the provider used for recognition.
func (c Config) STTProvider() string { return cmp.Or(c.Recognizer, c.Provider, DefaultProvider) }

// TTSProvider is the provider used for synthesis.
func (c Config) TTSProvider() string { return cmp.Or(c.Synthesizer, c.Provider, DefaultProvider) }

// KeepTranscripts reports whether transcripts are stored.
func (c Config) KeepTranscripts() bool { return c.StoreTranscripts == nil || *c.StoreTranscripts }

// Location returns the owner's timezone, or the local one.
func (c Config) Location() *time.Location {
	if c.Owner.Timezone != "" {
		if loc, err := time.LoadLocation(c.Owner.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

var (
	e164RE     = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	languageRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
)

// ValidE164 reports whether n is an E.164 phone number.
func ValidE164(n string) bool { return e164RE.MatchString(n) }

// Validate checks the configuration. A disabled config only has to be
// well-formed; an enabled one must be complete. Secrets are resolved
// through env to prove they exist, but never returned.
func (c Config) Validate(env func(string) string) error {
	if env == nil {
		env = os.Getenv
	}
	c = c.WithDefaults()
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if !languageRE.MatchString(c.Language) {
		add("language %q is not a BCP-47 tag such as tr-TR", c.Language)
	}
	if c.MaxCallSeconds < 10 || c.MaxCallSeconds > maxCallSecondsLimit {
		add("max_call_seconds must be between 10 and %d", maxCallSecondsLimit)
	}
	if c.MaxDailyMinutes < 1 || c.MaxDailyMinutes > maxDailyMinutesLimit {
		add("max_daily_minutes must be between 1 and %d", maxDailyMinutesLimit)
	}
	if c.MaxConcurrentCalls < 1 || c.MaxConcurrentCalls > 8 {
		add("max_concurrent_calls must be between 1 and 8")
	}
	if c.SilenceTimeoutSeconds < 3 || c.SilenceTimeoutSeconds > c.MaxCallSeconds {
		add("silence_timeout_seconds must be between 3 and max_call_seconds")
	}
	if c.TurnTimeoutSeconds < 5 || c.TurnTimeoutSeconds > 600 {
		add("turn_timeout_seconds must be between 5 and 600")
	}
	if c.MaxToolCallsPerTurn < 0 || c.MaxToolCallsPerTurn > 50 {
		add("max_tool_calls_per_turn must be between 0 and 50")
	}
	if c.RecordCalls {
		add("record_calls is not supported: Atlas never stores call audio")
	}
	if c.Outbound.MaxPerHour < 1 || c.Outbound.MaxPerHour > 20 {
		add("outbound.max_per_hour must be between 1 and 20")
	}
	if c.Outbound.RingTimeoutSeconds < 5 || c.Outbound.RingTimeoutSeconds > 120 {
		add("outbound.ring_timeout_seconds must be between 5 and 120")
	}
	for _, ch := range c.Outbound.Fallback {
		if ch != ChannelSMS && ch != ChannelWhatsAppText && ch != ChannelTelegram {
			add("outbound.fallback channel %q is not one of sms, whatsapp, telegram", ch)
		}
	}
	for _, n := range c.Owner.AllowedCallers {
		if !ValidE164(n) {
			add("owner.allowed_callers entry %s is not an E.164 number", MaskPhone(n))
		}
	}
	if c.Owner.Timezone != "" {
		if _, err := time.LoadLocation(c.Owner.Timezone); err != nil {
			add("owner.timezone %q is unknown", c.Owner.Timezone)
		}
	}
	for _, ref := range []struct {
		name string
		ref  SecretRef
	}{
		{"elevenlabs.api_key", c.ElevenLabs.APIKey},
		{"twilio.account_sid", c.Twilio.AccountSID},
		{"twilio.auth_token", c.Twilio.AuthToken},
		{"whatsapp.access_token", c.WhatsApp.AccessToken},
		{"whatsapp.app_secret", c.WhatsApp.AppSecret},
		{"whatsapp.verify_token", c.WhatsApp.VerifyToken},
		{"telegram.bot_token", c.Telegram.BotToken},
	} {
		if ref.ref != "" && !ref.ref.IsReference() {
			add("%s must be an environment reference such as $NAME, not the secret itself", ref.name)
		}
	}
	for _, s := range c.Monitor.Sites {
		if err := s.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	if !loopbackAddr(c.Listen.ControlAddr) {
		add("listen.control_addr must be a loopback address")
	}
	if !c.Enabled {
		return errors.Join(errs...)
	}

	if c.STTProvider() != "elevenlabs" || c.TTSProvider() != "elevenlabs" {
		add("only the elevenlabs provider is available in this build")
	}
	if c.Transport != "twilio" && c.Transport != "whatsapp" {
		add("transport must be twilio or whatsapp")
	}
	owner, err := c.OwnerNumber(env)
	switch {
	case err != nil:
		errs = append(errs, err)
	case owner == "":
		add("owner.phone_number is required")
	}
	if c.STTProvider() == "elevenlabs" || c.TTSProvider() == "elevenlabs" {
		if _, err := c.ElevenLabs.APIKey.Resolve(env); err != nil {
			add("elevenlabs.api_key: %v", err)
		}
		if c.ElevenLabs.VoiceID == "" {
			add("elevenlabs.voice_id is required")
		}
	}
	if c.Transport == "twilio" {
		if _, err := c.Twilio.AccountSID.Resolve(env); err != nil {
			add("twilio.account_sid: %v", err)
		}
		if _, err := c.Twilio.AuthToken.Resolve(env); err != nil {
			add("twilio.auth_token: %v", err)
		}
		if !ValidE164(c.Twilio.PhoneNumber) {
			add("twilio.phone_number must be an E.164 number")
		}
		u, err := url.Parse(c.Twilio.PublicBaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" {
			add("twilio.public_base_url must be an https URL without a query string")
		}
	}
	if c.Transport == "whatsapp" {
		for name, ref := range map[string]SecretRef{
			"whatsapp.access_token": c.WhatsApp.AccessToken,
			"whatsapp.app_secret":   c.WhatsApp.AppSecret,
			"whatsapp.verify_token": c.WhatsApp.VerifyToken,
		} {
			if _, err := ref.Resolve(env); err != nil {
				add("%s: %v", name, err)
			}
		}
		if !validPhoneNumberID(c.WhatsApp.PhoneNumberID) {
			add("whatsapp.phone_number_id must be the numeric Cloud API phone number id")
		}
		for _, s := range c.WhatsApp.STUNServers {
			if !strings.HasPrefix(s, "stun:") && !strings.HasPrefix(s, "stuns:") {
				add("whatsapp.stun_servers entry %q must start with stun: or stuns:", s)
			}
		}
	}
	for _, ch := range c.Outbound.Fallback {
		if c.Transport == "whatsapp" && ch != ChannelTelegram {
			add("outbound.fallback channel %q needs the twilio transport; use telegram with the whatsapp transport", ch)
			continue
		}
		switch ch {
		case ChannelTelegram:
			if _, err := c.Telegram.BotToken.Resolve(env); err != nil || c.Telegram.ChatID == "" {
				add("telegram fallback needs telegram.bot_token and telegram.chat_id")
			}
		case ChannelWhatsAppText:
			if !ValidE164(c.Twilio.WhatsAppFrom) {
				add("whatsapp fallback needs twilio.whatsapp_from")
			}
		}
	}
	return errors.Join(errs...)
}

// OwnerNumber resolves the owner's phone number.
func (c Config) OwnerNumber(env func(string) string) (string, error) {
	raw := strings.TrimSpace(c.Owner.PhoneNumber)
	if raw == "" {
		return "", nil
	}
	if SecretRef(raw).IsReference() {
		s, err := SecretRef(raw).Resolve(env)
		if err != nil {
			return "", fmt.Errorf("owner.phone_number: %w", err)
		}
		raw = s.Reveal()
	}
	if !ValidE164(raw) {
		return "", fmt.Errorf("owner.phone_number is not an E.164 number")
	}
	return raw, nil
}

// AllowedCaller reports whether number may call in.
func (c Config) AllowedCaller(number string, env func(string) string) bool {
	if !ValidE164(number) {
		return false
	}
	if owner, err := c.OwnerNumber(env); err == nil && owner == number {
		return true
	}
	for _, n := range c.Owner.AllowedCallers {
		if n == number {
			return true
		}
	}
	return false
}

func (s SiteConfig) validate() error {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("monitor site %q needs an http(s) URL", s.Name)
	}
	if u.User != nil {
		return fmt.Errorf("monitor site %q must not carry credentials in its URL", s.Name)
	}
	if strings.TrimSpace(s.Name) == "" || len(s.Name) > 64 {
		return fmt.Errorf("monitor site names must be 1-64 characters")
	}
	if s.IntervalSeconds != 0 && (s.IntervalSeconds < 15 || s.IntervalSeconds > 86400) {
		return fmt.Errorf("monitor site %q interval must be between 15 and 86400 seconds", s.Name)
	}
	if s.FailureThreshold < 0 || s.FailureThreshold > 10 {
		return fmt.Errorf("monitor site %q failure_threshold must be between 1 and 10", s.Name)
	}
	if s.Priority != "" && s.Priority != PriorityNormal && s.Priority != PriorityUrgent {
		return fmt.Errorf("monitor site %q priority must be normal or urgent", s.Name)
	}
	return nil
}

func validPhoneNumberID(id string) bool {
	if len(id) < 5 || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func loopbackAddr(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// SecretRef points at a secret instead of holding it: "$NAME", "${NAME}"
// or "env:NAME".
type SecretRef string

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsReference reports whether r is a well-formed reference.
func (r SecretRef) IsReference() bool { return r.envName() != "" }

func (r SecretRef) envName() string {
	s := strings.TrimSpace(string(r))
	switch {
	case strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}"):
		s = s[2 : len(s)-1]
	case strings.HasPrefix(s, "$"):
		s = s[1:]
	case strings.HasPrefix(s, "env:"):
		s = s[4:]
	default:
		return ""
	}
	if !envNameRE.MatchString(s) {
		return ""
	}
	return s
}

// Resolve reads the referenced secret.
func (r SecretRef) Resolve(env func(string) string) (Secret, error) {
	if env == nil {
		env = os.Getenv
	}
	name := r.envName()
	if name == "" {
		if r == "" {
			return Secret{}, errors.New("not set")
		}
		return Secret{}, errors.New("must be an environment reference such as $NAME")
	}
	v := strings.TrimSpace(env(name))
	if v == "" {
		return Secret{}, fmt.Errorf("environment variable %s is empty", name)
	}
	return Secret{v: v}, nil
}

// Secret is a resolved credential. Every printing path redacts it.
type Secret struct{ v string }

// NewSecret wraps a value, for tests and resolvers.
func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the value; call it only where the secret is sent.
func (s Secret) Reveal() string { return s.v }

// Empty reports whether no value is held.
func (s Secret) Empty() bool { return s.v == "" }

func (s Secret) String() string { return "[REDACTED]" }

// GoString implements fmt.GoStringer.
func (s Secret) GoString() string { return "voice.Secret([REDACTED])" }

// MarshalJSON implements json.Marshaler.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// MaskPhone hides all but the last two digits of a number.
func MaskPhone(n string) string {
	if len(n) <= 4 {
		return "***"
	}
	return n[:3] + strings.Repeat("*", len(n)-5) + n[len(n)-2:]
}
