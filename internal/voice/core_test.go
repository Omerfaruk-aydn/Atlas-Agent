package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMulawRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []int16{0, 1, -1, 100, -100, 1000, -1000, 8000, -8000, 32000, -32000, 32767, -32768} {
		got := MulawDecode(MulawEncode(s))
		diff := int(got) - int(s)
		if diff < 0 {
			diff = -diff
		}
		// mu-law keeps roughly 2-3% relative precision.
		require.LessOrEqual(t, diff, max(16, abs(int(s))/16), "sample %d decoded as %d", s, got)
	}
	// Silence encodes to the canonical 0xFF byte.
	require.Equal(t, byte(0xFF), MulawEncode(0))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestConvertAndValidate(t *testing.T) {
	t.Parallel()
	mu := AudioFrame{Encoding: EncodingMulaw8k, Data: bytes.Repeat([]byte{0xFF}, 160), TurnID: "t1"}
	pcm, err := Convert(mu, EncodingPCM16k)
	require.NoError(t, err)
	require.Equal(t, EncodingPCM16k, pcm.Encoding)
	require.Len(t, pcm.Data, 160*2*2, "8 kHz mu-law to 16 kHz PCM doubles samples and bytes per sample")
	require.Equal(t, "t1", pcm.TurnID)
	back, err := Convert(pcm, EncodingMulaw8k)
	require.NoError(t, err)
	require.Len(t, back.Data, 160)

	require.Error(t, AudioFrame{Encoding: "opus", Data: []byte{1}}.Validate())
	require.Error(t, AudioFrame{Encoding: EncodingPCM16k, Data: []byte{1, 2, 3}}.Validate(), "odd PCM byte count")
	require.Error(t, AudioFrame{Encoding: EncodingMulaw8k, Data: make([]byte, EncodingMulaw8k.MaxFrameBytes()+1)}.Validate())
	_, err = Convert(AudioFrame{Encoding: EncodingPCM16k, Data: []byte{1}}, EncodingMulaw8k)
	require.Error(t, err)
}

func testEnv(vals map[string]string) func(string) string {
	return func(k string) string { return vals[k] }
}

func completeConfig() Config {
	return Config{
		Enabled:    true,
		Owner:      OwnerConfig{PhoneNumber: "$OWNER"},
		ElevenLabs: ElevenLabsConfig{APIKey: "$EL_KEY", VoiceID: "voice1"},
		Twilio: TwilioConfig{
			AccountSID: "$TW_SID", AuthToken: "${TW_TOKEN}", PhoneNumber: "+15550001111",
			PublicBaseURL: "https://voice.example.com",
		},
	}
}

var completeEnv = testEnv(map[string]string{"OWNER": "+905551112233", "EL_KEY": "el-secret", "TW_SID": "AC123", "TW_TOKEN": "tw-secret"})

func TestConfigDefaultsAreSafe(t *testing.T) {
	t.Parallel()
	var cfg Config
	require.False(t, cfg.Enabled)
	require.NoError(t, cfg.Validate(testEnv(nil)), "an empty disabled config is valid")
	d := cfg.WithDefaults()
	require.Equal(t, 180, d.MaxCallSeconds)
	require.Equal(t, 30, d.MaxDailyMinutes)
	require.Equal(t, 1, d.MaxConcurrentCalls)
	require.False(t, d.RecordCalls)
	require.True(t, d.KeepTranscripts())
	require.Equal(t, "tr-TR", d.Language)
	require.True(t, loopbackAddr(d.Listen.ControlAddr))
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()
	require.NoError(t, completeConfig().Validate(completeEnv))

	cases := map[string]struct {
		mod  func(*Config)
		want string
	}{
		"missing owner":     {func(c *Config) { c.Owner.PhoneNumber = "" }, "owner.phone_number is required"},
		"raw api key":       {func(c *Config) { c.ElevenLabs.APIKey = "sk_live_abc" }, "elevenlabs.api_key must be an environment reference"},
		"raw twilio token":  {func(c *Config) { c.Twilio.AuthToken = "abcdef" }, "twilio.auth_token must be an environment reference"},
		"http base url":     {func(c *Config) { c.Twilio.PublicBaseURL = "http://voice.example.com" }, "public_base_url must be an https URL"},
		"query in base url": {func(c *Config) { c.Twilio.PublicBaseURL = "https://x.example.com/?token=1" }, "public_base_url"},
		"bad duration":      {func(c *Config) { c.MaxCallSeconds = 5 }, "max_call_seconds"},
		"recording":         {func(c *Config) { c.RecordCalls = true }, "record_calls is not supported"},
		"public control":    {func(c *Config) { c.Listen.ControlAddr = "0.0.0.0:8788" }, "loopback"},
		"bad caller":        {func(c *Config) { c.Owner.AllowedCallers = []string{"12345"} }, "allowed_callers"},
		"missing voice":     {func(c *Config) { c.ElevenLabs.VoiceID = "" }, "voice_id is required"},
		"unknown fallback":  {func(c *Config) { c.Outbound.Fallback = []string{"pigeon"} }, "pigeon"},
		"telegram fallback": {func(c *Config) { c.Outbound.Fallback = []string{ChannelTelegram} }, "telegram fallback needs"},
		"monitor creds":     {func(c *Config) { c.Monitor.Sites = []SiteConfig{{Name: "a", URL: "https://u:p@x.example.com"}} }, "credentials"},
		"other provider":    {func(c *Config) { c.Provider = "deepgram" }, "only the elevenlabs provider"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := completeConfig()
			tc.mod(&cfg)
			err := cfg.Validate(completeEnv)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.NotContains(t, err.Error(), "sk_live_abc")
			require.NotContains(t, err.Error(), "abcdef")
		})
	}

	t.Run("empty env var", func(t *testing.T) {
		t.Parallel()
		err := completeConfig().Validate(testEnv(map[string]string{"OWNER": "+905551112233"}))
		require.ErrorContains(t, err, "environment variable EL_KEY is empty")
	})
}

func whatsAppConfig() Config {
	c := completeConfig()
	c.Transport = "whatsapp"
	c.Twilio = TwilioConfig{}
	c.WhatsApp = WhatsAppConfig{
		AccessToken: "$WA_TOKEN", AppSecret: "$WA_SECRET", VerifyToken: "$WA_VERIFY",
		PhoneNumberID: "104560001",
	}
	return c
}

var whatsAppEnv = testEnv(map[string]string{
	"OWNER": "+905551112233", "EL_KEY": "el-secret",
	"WA_TOKEN": "wa-token", "WA_SECRET": "wa-secret", "WA_VERIFY": "wa-verify",
})

func TestWhatsAppConfigValidation(t *testing.T) {
	t.Parallel()
	require.NoError(t, whatsAppConfig().Validate(whatsAppEnv), "a whatsapp transport needs no twilio settings")

	d := whatsAppConfig().WithDefaults()
	require.Equal(t, DefaultWhatsAppGraphVersion, d.WhatsApp.GraphVersion)
	require.NotEmpty(t, d.WhatsApp.STUNServers)

	cases := map[string]struct {
		mod  func(*Config)
		want string
	}{
		"raw token":           {func(c *Config) { c.WhatsApp.AccessToken = "EAAB123" }, "whatsapp.access_token must be an environment reference"},
		"raw app secret":      {func(c *Config) { c.WhatsApp.AppSecret = "abc123" }, "whatsapp.app_secret must be an environment reference"},
		"missing verify":      {func(c *Config) { c.WhatsApp.VerifyToken = "" }, "whatsapp.verify_token"},
		"empty env":           {func(c *Config) { c.WhatsApp.AccessToken = "$NOPE" }, "whatsapp.access_token"},
		"phone instead of id": {func(c *Config) { c.WhatsApp.PhoneNumberID = "+905551112233" }, "phone_number_id"},
		"missing id":          {func(c *Config) { c.WhatsApp.PhoneNumberID = "" }, "phone_number_id"},
		"bad stun":            {func(c *Config) { c.WhatsApp.STUNServers = []string{"http://x"} }, "stun_servers"},
		"sms fallback":        {func(c *Config) { c.Outbound.Fallback = []string{ChannelSMS} }, "needs the twilio transport"},
		"unknown transport":   {func(c *Config) { c.Transport = "carrier-pigeon" }, "transport must be"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := whatsAppConfig()
			tc.mod(&cfg)
			err := cfg.Validate(whatsAppEnv)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
			require.NotContains(t, err.Error(), "EAAB123")
			require.NotContains(t, err.Error(), "abc123")
		})
	}
}

func TestSecretNeverPrints(t *testing.T) {
	t.Parallel()
	s, err := SecretRef("$K").Resolve(testEnv(map[string]string{"K": "super-secret-value"}))
	require.NoError(t, err)
	require.Equal(t, "super-secret-value", s.Reveal())
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s)} {
		require.NotContains(t, out, "super-secret-value")
	}
	b, err := json.Marshal(struct{ K Secret }{s})
	require.NoError(t, err)
	require.NotContains(t, string(b), "super-secret-value")
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("Test", "key", s)
	require.NotContains(t, buf.String(), "super-secret-value")

	require.Equal(t, "+90********33", MaskPhone("+905551112233"))
}

func TestOwnerAndAllowlist(t *testing.T) {
	t.Parallel()
	cfg := completeConfig()
	cfg.Owner.AllowedCallers = []string{"+15557778888"}
	n, err := cfg.OwnerNumber(completeEnv)
	require.NoError(t, err)
	require.Equal(t, "+905551112233", n)
	require.True(t, cfg.AllowedCaller("+905551112233", completeEnv))
	require.True(t, cfg.AllowedCaller("+15557778888", completeEnv))
	require.False(t, cfg.AllowedCaller("+15550000000", completeEnv))
	require.False(t, cfg.AllowedCaller("anonymous", completeEnv))
	require.False(t, cfg.AllowedCaller("", completeEnv))
}

func TestStateMachine(t *testing.T) {
	t.Parallel()
	m := NewMachine(nil)
	require.Equal(t, StateIdle, m.State())
	require.ErrorIs(t, m.Transition(StateSpeaking), ErrInvalidState)
	for _, s := range []State{StateRinging, StateConnecting, StateActive, StateListening, StateSpeaking, StateToolRunning, StateListening} {
		require.NoError(t, m.Transition(s))
	}
	require.NoError(t, m.Transition(StateListening), "re-entering is a no-op")
	require.Equal(t, StateEnded, m.Finish(StateEnded))
	select {
	case <-m.Done():
	default:
		t.Fatal("done not closed")
	}
	require.Equal(t, StateEnded, m.Finish(StateFailed), "the first terminal state sticks")
	require.ErrorIs(t, m.Transition(StateActive), ErrInvalidState)
	hist := m.History()
	require.Equal(t, StateEnding, hist[len(hist)-2].To, "conversation ends through ending")

	ring := NewMachine(nil)
	require.NoError(t, ring.Transition(StateRinging))
	require.Equal(t, StateNoAnswer, ring.Finish(StateNoAnswer))
	other := NewMachine(nil)
	require.NoError(t, other.Transition(StateRinging))
	require.Equal(t, StateCancelled, other.Finish(StateEnded), "an impossible terminal state falls back to cancelled")
	require.False(t, other.TransitionFrom(StateActive, StateRinging))
}

func TestToolPolicy(t *testing.T) {
	t.Parallel()
	readOnly := func(n string) bool { return n == "grep" }
	p := NewToolPolicy(ToolPolicyConfig{Allow: []string{"mcp_gmail_search*", "bash"}, Block: []string{"mcp_calendar_delete*"}}, readOnly)
	require.Equal(t, ToolAllow, p.Decide("grep"))
	require.Equal(t, ToolAllow, p.Decide("mcp_gmail_search_messages"))
	require.Equal(t, ToolBlock, p.Decide("bash"), "an allow entry cannot reopen a default block")
	require.Equal(t, ToolBlock, p.Decide("mcp_gmail_send_email"))
	require.Equal(t, ToolBlock, p.Decide("MCP_Gmail_Send_Message"))
	require.Equal(t, ToolBlock, p.Decide("mcp_calendar_delete_event"))
	require.Equal(t, ToolBlock, p.Decide("call_owner"), "no recursive calling from a call")
	require.Equal(t, ToolConfirm, p.Decide("mcp_gmail_create_draft"))
	require.Equal(t, ToolConfirm, p.Decide("mcp_calendar_create_event"))

	lifted := NewToolPolicy(ToolPolicyConfig{AllowEmailSend: true}, nil)
	require.Equal(t, ToolConfirm, lifted.Decide("mcp_gmail_send_email"), "sending still needs confirmation")
}

func TestParseAnswer(t *testing.T) {
	t.Parallel()
	yes := []string{"Evet", "evet onaylıyorum", "Onaylıyorum.", "tamam", "yes", "Yes, confirm"}
	no := []string{"hayır", "Hayır, iptal", "evet ama yarın değil", "no", "don't", "yes but not now"}
	unclear := []string{"", "hmm", "bir dakika bekle", "evet sanırım öyle olması lazım bence de"}
	for _, s := range yes {
		require.Equal(t, AnswerYes, ParseAnswer(s), s)
	}
	for _, s := range no {
		require.Equal(t, AnswerNo, ParseAnswer(s), s)
	}
	for _, s := range unclear {
		require.Equal(t, AnswerUnclear, ParseAnswer(s), s)
	}
}

func TestSummarizeToolCall(t *testing.T) {
	t.Parallel()
	in := `{"summary":"Ahmet ile toplantı","start":{"dateTime":"2026-11-02T10:00:00+03:00"},"end":{"dateTime":"2026-11-02T10:30:00+03:00"},"timeZone":"Europe/Istanbul","attendees":[{"email":"ahmet@example.com"}],"colorId":"5"}`
	got := SummarizeToolCall("mcp_calendar_create_event", in)
	for _, want := range []string{"calendar create event", "Ahmet ile toplantı", "2026-11-02T10:00:00+03:00", "2026-11-02T10:30:00+03:00", "Europe/Istanbul", "ahmet@example.com"} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, "colorId")
	require.Equal(t, "edit", SummarizeToolCall("edit", "not json"))
}

func TestTurnAuthorize(t *testing.T) {
	t.Parallel()
	policy := NewToolPolicy(ToolPolicyConfig{}, func(n string) bool { return n == "view" })
	asked := 0
	turn := &Turn{Policy: policy, MaxSteps: 3, Confirm: func(_ context.Context, req ConfirmRequest) (bool, error) {
		asked++
		return req.ToolName == "mcp_calendar_create_event", nil
	}}
	ctx := context.Background()
	a, err := turn.Authorize(ctx, "view", "{}")
	require.NoError(t, err)
	require.True(t, a.Run)
	require.False(t, a.Confirmed)

	a, _ = turn.Authorize(ctx, "bash", "{}")
	require.False(t, a.Run)
	require.Contains(t, a.Reason, "cannot be used during a phone call")

	a, _ = turn.Authorize(ctx, "mcp_calendar_create_event", "{}")
	require.True(t, a.Run)
	require.True(t, a.Confirmed)

	a, _ = turn.Authorize(ctx, "mcp_tasks_delete", "{}")
	require.False(t, a.Run, "step budget exhausted before asking")
	require.Equal(t, 1, asked)

	noConfirm := &Turn{Policy: policy}
	a, _ = noConfirm.Authorize(ctx, "mcp_calendar_create_event", "{}")
	require.False(t, a.Run, "without a confirmer nothing needing confirmation runs")

	got, ok := TurnFrom(WithTurn(ctx, turn))
	require.True(t, ok)
	require.Same(t, turn, got)
	_, ok = TurnFrom(ctx)
	require.False(t, ok)
}

func TestToSpoken(t *testing.T) {
	t.Parallel()
	p := PhrasesFor("tr-TR")
	in := "# Özet\n**Toplantı** yarın saat 10.30'da, süre 45 dakika.\n\n```go\nfmt.Println(1)\n```\n| a | b |\n|---|---|\n| 1 | 2 |\n\n- bir\n- iki\n- üç\n- dört\n- beş\n\nDetay: [rapor](https://example.com/x) ve https://example.com/y"
	got := ToSpoken(in, p, 0)
	require.Contains(t, got, "Toplantı yarın saat 10.30'da, süre 45 dakika.")
	require.Contains(t, got, p.CodeOmitted)
	require.Contains(t, got, p.TableOmitted)
	require.Contains(t, got, "ve 2 madde daha.")
	require.Contains(t, got, "Detay: rapor ve bağlantı")
	for _, banned := range []string{"```", "|", "**", "#", "http", "fmt.Println", "dört"} {
		require.NotContains(t, got, banned)
	}
	long := strings.Repeat("Bu bir cümle. ", 100)
	short := ToSpoken(long, p, 50)
	require.LessOrEqual(t, len([]rune(short)), 51)
	require.True(t, strings.HasSuffix(short, "."), short)
	require.Equal(t, "English", PhrasesFor("de-DE").Greeting[:0]+"English")
}

func TestSplitSegments(t *testing.T) {
	t.Parallel()
	in := "Saat 10.30'da başlıyor. Fiyat 3.5 lira! Kısa. Bu cümle ise yeterince uzun bir cümle olmalı?"
	segs := SplitSegments(in)
	require.Len(t, segs, 2)
	require.Equal(t, in, strings.Join(segs, " "), "segmenting never changes the text")
	for _, s := range segs {
		require.False(t, strings.HasSuffix(s, "10.") || strings.HasSuffix(s, "3."), "split inside a number: %q", s)
		require.GreaterOrEqual(t, len([]rune(s)), minSegmentRunes)
	}
	long := strings.Repeat("kelime ", 100)
	for _, s := range SplitSegments(long) {
		require.LessOrEqual(t, len([]rune(s)), maxSegmentRunes)
	}
	require.Empty(t, SplitSegments("   "))
}

func TestStoreIdempotencyAndPersistence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := OpenStore(dir)
	require.NoError(t, err)
	now := time.Now()
	require.NoError(t, s.Create(CallRecord{ID: "c1", ProviderCallID: "CA1", Direction: DirectionOutbound, CreatedAt: now, AnsweredAt: now.Add(-90 * time.Second), EndedAt: now}))
	require.ErrorIs(t, s.Create(CallRecord{ID: "c2", ProviderCallID: "CA1"}), ErrCallExists)
	require.ErrorIs(t, s.Create(CallRecord{ID: "c1"}), ErrCallExists)

	_, claimed, err := s.ClaimEvent("evt", "c1")
	require.NoError(t, err)
	require.True(t, claimed)
	id, claimed, err := s.ClaimEvent("evt", "c9")
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, "c1", id)
	require.NoError(t, s.ReleaseEvent("evt", "c9"), "a non-owner release is ignored")
	_, claimed, _ = s.ClaimEvent("evt", "c9")
	require.False(t, claimed)

	require.Equal(t, 90, s.SecondsSince(now.Add(-time.Hour), now))
	require.Len(t, s.OutboundSince(now.Add(-time.Minute)), 1)

	reopened, err := OpenStore(dir)
	require.NoError(t, err)
	rec, ok := reopened.Get("c1")
	require.True(t, ok)
	require.Equal(t, "CA1", rec.ProviderCallID)
	_, claimed, _ = reopened.ClaimEvent("evt", "c3")
	require.False(t, claimed, "claims survive a restart")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "calls.json"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestBudget(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	cfg := Config{MaxDailyMinutes: 2, MaxCallSeconds: 100, Owner: OwnerConfig{Timezone: "UTC"}}
	b := NewBudget(cfg, store, func() time.Time { return now })

	limit, release, err := b.Admit("a")
	require.NoError(t, err)
	require.Equal(t, 100*time.Second, limit)
	_, _, err = b.Admit("b")
	require.ErrorIs(t, err, ErrBudgetExceeded, "one concurrent call by default")
	_, _, err = b.Admit("a")
	require.ErrorIs(t, err, ErrCallExists)
	release()
	release()
	require.Equal(t, 0, b.Active())

	require.NoError(t, store.Create(CallRecord{ID: "x", CreatedAt: now.Add(-time.Hour), AnsweredAt: now.Add(-time.Hour), EndedAt: now.Add(-time.Hour + 110*time.Second)}))
	require.Equal(t, 10*time.Second, b.RemainingToday())
	_, _, err = b.Admit("c")
	require.ErrorIs(t, err, ErrBudgetExceeded, "too little time left today for a useful call")

	// Outbound rate.
	store2 := NewMemoryStore()
	b2 := NewBudget(Config{Outbound: OutboundConfig{MaxPerHour: 2, MinIntervalSeconds: 600}}, store2, func() time.Time { return now })
	require.NoError(t, b2.AllowOutbound(PriorityNormal))
	require.NoError(t, store2.Create(CallRecord{ID: "o1", Direction: DirectionOutbound, CreatedAt: now.Add(-time.Minute)}))
	require.ErrorIs(t, b2.AllowOutbound(PriorityNormal), ErrRateLimited, "minimum gap")
	require.NoError(t, b2.AllowOutbound(PriorityUrgent), "urgent skips the gap")
	require.NoError(t, store2.Create(CallRecord{ID: "o2", Direction: DirectionOutbound, CreatedAt: now.Add(-30 * time.Second)}))
	require.ErrorIs(t, b2.AllowOutbound(PriorityUrgent), ErrRateLimited, "urgent never skips the hourly cap")
}
