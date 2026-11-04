package integrations

import (
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/localapps"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
)

// NewDefault wires every adapter for a running Atlas: providers, accounts,
// MCP servers, local tools, local app runtimes and local apps. It also makes
// the app manager use this service for runtime checks.
func NewDefault(store *config.ConfigStore, apps *localapps.Manager) *Service {
	s := New(Options{StatePath: filepath.Join(filepath.Dir(config.GlobalConfigData()), "integrations", "checks.json")})
	prober := NewProber()
	tools := DefaultTools(browser.FindChrome)
	s.Add(&ProviderAdapter{Backend: store, Secrets: s.Secrets()})
	s.Add(&MCPAdapter{Backend: store, Runtime: LiveMCP{Store: store}, Secrets: s.Secrets()})
	s.Add(&ToolAdapter{Specs: tools, Prober: prober})
	s.Add(&DependencyAdapter{Runtimes: Runtimes(), Tools: tools, Prober: prober})
	s.Add(&VoiceAdapter{
		Config:  func() voice.Config { return voice.Config(store.Config().Tools.VoiceCalls) },
		DataDir: filepath.Dir(config.GlobalConfigData()),
		Env:     connections.Env(filepath.Dir(config.GlobalConfigData())),
	})
	if apps != nil {
		s.Add(&AppAdapter{Apps: apps})
		apps.SetRuntimeChecker(s)
	}
	return s
}
