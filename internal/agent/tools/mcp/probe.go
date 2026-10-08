package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

// ProbeResult is what a probe observed about one server.
type ProbeResult struct {
	// Tools are the names the server advertised, after the configured
	// allow and deny lists.
	Tools  []string
	Counts Counts
	// Reconnected is true when the probe had to connect instead of
	// reusing a healthy session.
	Reconnected bool
}

// ErrProbeDisabled is returned for a server disabled in configuration.
var ErrProbeDisabled = errors.New("mcp server is disabled")

// ErrProbeNeedsAuth is returned when the server needs an OAuth sign-in.
var ErrProbeNeedsAuth = errors.New("mcp server needs authorization")

// Probe checks one configured server. A healthy session is pinged and its
// tool list is fetched again; otherwise a connection is made, which runs
// the handshake and tool discovery and registers the tools as a normal
// start would. It never opens a browser for OAuth.
func Probe(ctx context.Context, cfg *config.ConfigStore, name string) (ProbeResult, error) {
	m, ok := cfg.Config().MCP[name]
	if !ok {
		return ProbeResult{}, fmt.Errorf("mcp '%s' not found in configuration", name)
	}
	if m.Disabled {
		return ProbeResult{}, ErrProbeDisabled
	}
	if sess, ok := sessions.Get(name); ok {
		if err := pingSession(ctx, sess, mcpTimeout(m)); err == nil {
			tools, err := getTools(ctx, sess)
			if err != nil {
				return ProbeResult{}, fmt.Errorf("list tools: %w", err)
			}
			filtered := filterTools(m, tools)
			state, _ := GetState(name)
			return ProbeResult{Tools: toolNames(filtered), Counts: state.Counts}, nil
		}
		if _, err := getOrRenewClient(ctx, cfg, name); err != nil {
			return ProbeResult{Reconnected: true}, probeErr(name, err)
		}
	} else if err := InitializeSingle(ctx, name, cfg); err != nil {
		return ProbeResult{Reconnected: true}, probeErr(name, err)
	}
	state, _ := GetState(name)
	switch state.State {
	case StateConnected:
		tools, _ := allTools.Get(name)
		return ProbeResult{Tools: toolNames(tools), Counts: state.Counts, Reconnected: true}, nil
	case StateNeedsAuth:
		return ProbeResult{Reconnected: true}, ErrProbeNeedsAuth
	default:
		if state.Error != nil {
			return ProbeResult{Reconnected: true}, state.Error
		}
		return ProbeResult{Reconnected: true}, fmt.Errorf("mcp '%s' did not connect (%s)", name, state.State)
	}
}

func probeErr(name string, err error) error {
	if state, ok := GetState(name); ok && state.State == StateNeedsAuth {
		return ErrProbeNeedsAuth
	}
	return err
}

func toolNames(tools []*Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if t != nil {
			names = append(names, t.Name)
		}
	}
	slices.Sort(names)
	return names
}
