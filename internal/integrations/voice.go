package integrations

import (
	"context"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// VoiceAdapter reports the phone voice assistant. Listing and checking
// read configuration and, when the service runs, ask its loopback API for
// status; neither ever places a call or contacts a paid service.
type VoiceAdapter struct {
	// Config returns the current voice configuration.
	Config func() voice.Config
	// DataDir holds the running service's control state.
	DataDir string
	// Env resolves secret references; os.Getenv when nil.
	Env func(string) string
}

const voiceItemID = "tool:voice-calls"

func (a *VoiceAdapter) env() func(string) string {
	if a.Env != nil {
		return a.Env
	}
	return os.Getenv
}

// Items implements Adapter.
func (a *VoiceAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	cfg := a.Config()
	it := Item{
		ID: voiceItemID, Name: i18n.M("Phone voice assistant").String(), Kind: KindTool,
		Description: i18n.M("Talk to Atlas by phone and let it call you (Twilio and ElevenLabs)."),
		Source:      Source{Origin: "atlas.json", Scope: "global", ConfigKey: "tools.voice_calls"},
		Capabilities: []i18n.Message{
			i18n.M("Inbound and outbound phone calls"),
			i18n.M("Site monitoring with calls on state changes"),
		},
		Auth:    AuthEnv,
		Actions: []Action{ActionTest},
	}
	it.fingerprint = fp.Fingerprint(voiceItemID, cfg)
	switch {
	case !cfg.Enabled && cfg.Owner.PhoneNumber == "" && cfg.Twilio.PhoneNumber == "":
		it.Status = StatusNotConfigured
		it.Remedy = i18n.M("Configure tools.voice_calls; see docs/voice-calls.md.")
		it.Actions = nil
	case !cfg.Enabled:
		it.Status = StatusDisabled
		it.Detail = i18n.M("Turned off in tools.voice_calls.")
		it.Actions = nil
	default:
		it.Status = StatusUnchecked
	}
	return []Item{it}
}

// Check implements Adapter: configuration validation plus a status request
// to the running service, if any.
func (a *VoiceAdapter) Check(ctx context.Context, _ Item) CheckResult {
	cfg := a.Config()
	if !cfg.Enabled {
		return CheckResult{Status: StatusDisabled, Detail: i18n.M("Turned off in tools.voice_calls.")}
	}
	if err := cfg.Validate(a.env()); err != nil {
		return CheckResult{
			Status: StatusConnectionError, Err: i18n.Raw(err.Error()),
			Remedy: i18n.M("Fix the voice configuration and run atlas-agent phone check."),
		}
	}
	client, err := voice.DialControl(a.DataDir)
	if err == nil {
		if _, err = client.Status(ctx); err == nil {
			return CheckResult{Status: StatusReady, Detail: i18n.M("The voice service is running.")}
		}
	}
	return CheckResult{
		Status: StatusLimited, Detail: i18n.M("The configuration is complete but the voice service is not running."),
		Remedy: i18n.M("Start it with atlas-agent phone serve."),
	}
}

// Do implements Adapter. Test calls are never placed from here: they ring a
// phone and are billed, so they stay an explicit CLI action.
func (a *VoiceAdapter) Do(context.Context, Item, Action) (i18n.Message, error) {
	return i18n.Message{}, ErrUnsupported
}
