package integrations

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools/mcp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// MCPRuntime is the part of the MCP client manager the adapter uses.
type MCPRuntime interface {
	States() map[string]mcp.ClientInfo
	Probe(ctx context.Context, name string) (mcp.ProbeResult, error)
	Start(ctx context.Context, name string) error
	Stop(name string) error
}

// LiveMCP is the MCPRuntime backed by Atlas's MCP client manager.
type LiveMCP struct{ Store *config.ConfigStore }

// States implements MCPRuntime.
func (l LiveMCP) States() map[string]mcp.ClientInfo { return mcp.GetStates() }

// Probe implements MCPRuntime.
func (l LiveMCP) Probe(ctx context.Context, name string) (mcp.ProbeResult, error) {
	return mcp.Probe(ctx, l.Store, name)
}

// Start implements MCPRuntime.
func (l LiveMCP) Start(ctx context.Context, name string) error {
	return mcp.InitializeSingle(ctx, name, l.Store)
}

// Stop implements MCPRuntime.
func (l LiveMCP) Stop(name string) error { return mcp.DisableSingle(l.Store, name) }

// MCPAdapter lists configured MCP servers with their live client state.
type MCPAdapter struct {
	Backend ConfigBackend
	Runtime MCPRuntime
	Secrets *Secrets
	// LookPath finds stdio commands; defaults to exec.LookPath.
	LookPath func(string) (string, error)
}

func (a *MCPAdapter) lookPath(name string) (string, error) {
	if a.LookPath != nil {
		return a.LookPath(name)
	}
	return exec.LookPath(name)
}

// Items implements Adapter.
func (a *MCPAdapter) Items(_ context.Context, fp Fingerprinter) []Item {
	cfg := a.Backend.Config()
	if cfg == nil {
		return nil
	}
	files := filesOf(a.Backend)
	states := map[string]mcp.ClientInfo{}
	if a.Runtime != nil {
		states = a.Runtime.States()
	}
	var out []Item
	for _, entry := range cfg.MCP.Sorted() {
		name, m := entry.Name, entry.MCP
		it := Item{
			ID:     "mcp:" + name,
			Name:   name,
			Kind:   KindMCP,
			Source: files.locate("mcp." + escapeKey(name)),
			Auth:   AuthNone,
		}
		if name == config.DockerMCPName {
			it.Name = "Docker MCP"
		}
		switch {
		case m.OAuth:
			it.Auth = AuthOAuth
		case len(m.Headers) > 0:
			it.Auth = AuthHeaders
		}
		if a.Secrets != nil {
			for _, v := range m.Headers {
				a.Secrets.Add(headerSecret(v))
			}
			for k, v := range m.Env {
				if looksSecretName(k) {
					a.Secrets.Add(v)
				}
			}
			if m.OAuthToken != nil {
				a.Secrets.Add(m.OAuthToken.AccessToken, m.OAuthToken.RefreshToken)
			}
		}
		switch m.Type {
		case config.MCPHttp, config.MCPSSE:
			it.Description = i18n.M("MCP server over %s.", strings.ToUpper(string(m.Type)))
		default:
			it.Description = i18n.M("MCP server started as a local program (%s).", m.Command)
		}
		if it.Source.Origin == "" {
			it.Source = Source{Origin: "atlas", Scope: "global"}
		}
		state, hasState := states[name]
		switch {
		case m.Disabled:
			it.Status = StatusDisabled
		case !hasState:
			it.Status = StatusUnchecked
		default:
			it.Live = true
			switch state.State {
			case mcp.StateStarting:
				it.Status = StatusChecking
			case mcp.StateConnected:
				it.Status = StatusReady
				it.Detail = mcpCountsText(state.Counts)
			case mcp.StateNeedsAuth:
				it.Status = StatusNeedsLogin
				it.Remedy = i18n.M("Authorize the server: choose Sign in.")
			case mcp.StateError:
				it.Status = StatusConnectionError
				if state.Error != nil {
					it.LastError = i18n.Raw(state.Error.Error())
				}
				it.Remedy = i18n.M("Check the server command or URL, then test again.")
			case mcp.StateDisabled:
				it.Status = StatusUnchecked
				it.Live = false
			}
		}
		if it.Status == StatusReady || it.Status == StatusChecking {
			it.Capabilities = mcpCapabilities(state.Counts)
		}
		if !m.Disabled {
			it.Actions = append(it.Actions, ActionTest)
			it.CheckNote = i18n.M("Connects, completes the MCP handshake and lists tools; no tool is called.")
			if m.OAuth && m.Type == config.MCPHttp {
				it.Actions = append(it.Actions, ActionLogin)
			}
		}
		if it.Source.Editable {
			if m.Disabled {
				it.Actions = append(it.Actions, ActionEnable)
			} else {
				it.Actions = append(it.Actions, ActionDisable)
			}
			it.Actions = append(it.Actions, ActionRemove)
		} else if it.Source.Path != "" {
			it.Remedy = i18n.Join(it.Remedy, definedIn(it.Source.Path))
		}
		it.LastError = it.LastError.Map(a.redact)
		it.fingerprint = fp.Fingerprint("mcp", name, m)
		out = append(out, it)
	}
	return out
}

func (a *MCPAdapter) redact(s string) string {
	if a.Secrets == nil {
		return s
	}
	return a.Secrets.Redact(s)
}

func headerSecret(v string) string {
	if _, rest, ok := strings.Cut(v, " "); ok && len(rest) >= minSecretLen {
		return rest
	}
	return v
}

func looksSecretName(k string) bool {
	k = strings.ToUpper(k)
	for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASS", "AUTH", "CREDENTIAL"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

func mcpCountsText(c mcp.Counts) i18n.Message {
	return i18n.M("Connected: %d tools, %d prompts, %d resources", c.Tools, c.Prompts, c.Resources)
}

func mcpCapabilities(c mcp.Counts) []i18n.Message {
	var caps []i18n.Message
	if c.Tools > 0 {
		caps = append(caps, i18n.M("%d tools", c.Tools))
	}
	if c.Prompts > 0 {
		caps = append(caps, i18n.M("%d prompts", c.Prompts))
	}
	if c.Resources > 0 {
		caps = append(caps, i18n.M("%d resources", c.Resources))
	}
	return caps
}

// Check implements Adapter.
func (a *MCPAdapter) Check(ctx context.Context, item Item) CheckResult {
	name := providerID(item.ID)
	m, ok := a.Backend.Config().MCP[name]
	if !ok {
		return CheckResult{Status: StatusNotConfigured}
	}
	if (m.Type == config.MCPStdio || m.Type == "") && m.Command != "" {
		if _, err := a.lookPath(m.Command); err != nil {
			return CheckResult{
				Status: StatusMissingDependency,
				Err:    i18n.M("The command %q was not found.", m.Command),
				Remedy: i18n.M("Install %s or put it on PATH, then test again.", m.Command),
			}
		}
	}
	if a.Runtime == nil {
		return CheckResult{Status: StatusUnchecked, Err: i18n.M("MCP client manager is not running.")}
	}
	res, err := a.Runtime.Probe(ctx, name)
	if ctx.Err() != nil {
		return CheckResult{}
	}
	switch {
	case errors.Is(err, mcp.ErrProbeNeedsAuth):
		return CheckResult{Status: StatusNeedsLogin, Err: i18n.M("The server asked for authorization."), Remedy: i18n.M("Authorize the server: choose Sign in.")}
	case errors.Is(err, mcp.ErrProbeDisabled):
		return CheckResult{Status: StatusDisabled}
	case err != nil:
		return CheckResult{Status: StatusConnectionError, Err: i18n.Raw(err.Error()), Remedy: i18n.M("Check the server command or URL and its logs, then test again.")}
	}
	caps := mcpCapabilities(res.Counts)
	if len(res.Tools) > 0 {
		shown := res.Tools
		list := strings.Join(shown, ", ")
		if len(shown) > 8 {
			list = strings.Join(shown[:8], ", ")
			caps = append(caps, i18n.M("tools: %s (+%d more)", list, len(res.Tools)-8))
		} else {
			caps = append(caps, i18n.M("tools: %s", list))
		}
	}
	detail := i18n.M("Handshake and tool discovery succeeded: %d tools.", len(res.Tools))
	if len(res.Tools) == 0 {
		return CheckResult{Status: StatusLimited, Detail: i18n.M("Connected, but the server offers no tools."), Capabilities: caps}
	}
	return CheckResult{Status: StatusReady, Detail: detail, Capabilities: caps}
}

// Do implements Adapter.
func (a *MCPAdapter) Do(ctx context.Context, item Item, action Action) (i18n.Message, error) {
	name := providerID(item.ID)
	base := "mcp." + escapeKey(name)
	scope := scopeFor(item.Source)
	switch action {
	case ActionLogin:
		return i18n.M("Authorize %s from the MCP sign-in prompt.", name), nil
	case ActionDisable, ActionEnable, ActionRemove:
		if !item.Source.Editable {
			return i18n.Message{}, errors.New("this server is defined outside the files Atlas edits")
		}
	default:
		return i18n.Message{}, ErrUnsupported
	}
	switch action {
	case ActionDisable:
		if err := a.Backend.SetConfigField(scope, base+".disabled", true); err != nil {
			return i18n.Message{}, fmt.Errorf("save %s: %w", item.Source.Path, err)
		}
		if a.Runtime != nil {
			_ = a.Runtime.Stop(name)
		}
		return i18n.M("Disabled in %s. Its settings and sign-in were kept.", item.Source.Path), nil
	case ActionEnable:
		if err := a.Backend.SetConfigField(scope, base+".disabled", false); err != nil {
			return i18n.Message{}, fmt.Errorf("save %s: %w", item.Source.Path, err)
		}
		if a.Runtime != nil {
			if err := a.Runtime.Start(ctx, name); err != nil {
				return i18n.M("Enabled in %s, but the server did not start: %s", item.Source.Path, a.redact(err.Error())), nil
			}
		}
		return i18n.M("Enabled in %s.", item.Source.Path), nil
	default:
		if a.Runtime != nil {
			_ = a.Runtime.Stop(name)
		}
		if err := a.Backend.RemoveConfigField(scope, base); err != nil {
			return i18n.Message{}, fmt.Errorf("remove from %s: %w", item.Source.Path, err)
		}
		return i18n.M("Removed the %s entry from %s.", name, item.Source.Path), nil
	}
}
