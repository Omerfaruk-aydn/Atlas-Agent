package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
)

// newSupportService builds the /support service. Its state (identity,
// operation journal, history) lives with the other device state, outside
// any workspace.
func newSupportService(store *config.ConfigStore) *support.Service {
	return support.NewService(filepath.Join(config.GlobalDevicesDir(), "support"), func() support.ServiceConfig {
		return supportConfig(store)
	}, slog.Default())
}

func supportConfig(store *config.ConfigStore) support.ServiceConfig {
	cfg := store.Config()
	devices := cfg.Tools.Devices
	out := support.ServiceConfig{Name: devices.SupportName}
	out.ExecutorFactory = func(dir string, backend computer.Backend) nodes.RemoteToolExecutor {
		return tools.NewSupportExecutor(dir, backend, cfg.Tools.Browser, func(root string, inner fantasy.AgentTool) fantasy.AgentTool {
			current := store.Config()
			return agent.WithToolHooks(inner, hooks.NewRunner(current.Hooks[hooks.EventPreToolUse], root, root), hooks.NewRunner(current.Hooks[hooks.EventPostToolUse], root, root))
		})
	}
	if cfg.Options != nil && cfg.Options.TUI != nil {
		out.Language = cfg.Options.TUI.Language
	}
	if devices.SupportNoDiscovery {
		out.DiscoveryPort = -1
	}
	if url := strings.TrimSpace(devices.SupportRelayURL); url != "" {
		relay := &support.RelayConfig{URL: url}
		if path := strings.TrimSpace(devices.SupportRelayTokenFile); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				// The relay then refuses the connection with a clear
				// token error; the path is logged, the token never is.
				slog.Warn("Cannot read the support relay token file", "path", path, "error", err)
			} else {
				relay.Token = strings.TrimSpace(string(data))
			}
		}
		out.Relay = relay
	}
	return out
}
