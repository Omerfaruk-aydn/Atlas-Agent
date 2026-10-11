package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

type supportExecution struct {
	gate     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	computer fantasy.AgentTool
	browser  fantasy.AgentTool
}

type supportExecutor struct {
	mu       sync.Mutex
	stateDir string
	backend  computer.Backend
	manager  *browser.Manager
	sessions map[string]*supportExecution
	closed   map[string]bool
	wrap     func(string, fantasy.AgentTool) fantasy.AgentTool
}

// NewSupportExecutor reuses the native tools on the supported computer.
// The authorization callback remains mandatory even with local auto-approve.
func NewSupportExecutor(stateDir string, backend computer.Backend, cfg config.ToolBrowser, wrappers ...func(string, fantasy.AgentTool) fantasy.AgentTool) nodes.RemoteToolExecutor {
	if cfg.Headless == nil {
		visible := false
		cfg.Headless = &visible
	}
	manager := browser.NewManager(browser.Options{IsolateProfiles: true, OverlayDisabled: true, ExecutablePath: cfg.ExecutablePath, Headless: cfg.IsHeadless(), UserDataDir: cfg.GetUserDataDir(), UseRealProfile: cfg.UsesRealProfile(), RealProfilePin: cfg.GetRealProfilePin(), RemoteURL: cfg.GetRemoteURL(), ActionTimeout: cfg.GetActionTimeout(), IdleTimeout: cfg.GetIdleTimeout()})
	e := &supportExecutor{stateDir: stateDir, backend: backend, manager: manager, sessions: map[string]*supportExecution{}, closed: map[string]bool{}}
	if len(wrappers) > 0 {
		e.wrap = wrappers[0]
	}
	return e
}

var supportBrowserActions = []string{"navigate", "back", "forward", "click", "type", "key", "scroll", "snapshot", "observe", "screenshot", "text", "html", "url", "find", "assert", "wait_for", "semantic_click", "semantic_type", "set_field", "read_field", "select_option", "fill_form", "submit", "collect", "tabs", "tab_new", "tab_select", "tab_close", "tab_return", "frames", "popup_wait", "dialog_wait", "dialog_handle", "health", "pointer_move", "pointer_click", "pointer_drag", "key_combo", "insert_text"}

func (e *supportExecutor) Validate(req nodes.RemoteToolRequest) (bool, error) {
	switch req.Tool {
	case BrowserToolName:
		var p BrowserParams
		if err := json.Unmarshal(req.Input, &p); err != nil {
			return false, err
		}
		if !slices.Contains(supportBrowserActions, p.Action) {
			return false, fmt.Errorf("unsupported remote browser action %q", p.Action)
		}
		if err := browserArgumentPreflight(p.Action, p, false); err != nil {
			return false, err
		}
		return browserMutating(p.Action), nil
	case ComputerToolName:
		var p ComputerParams
		if err := json.Unmarshal(req.Input, &p); err != nil {
			return false, err
		}
		if !slices.Contains(computerActions, p.Action) || p.Action == "batch" || p.Action == "handoff" || p.Action == "auth_code" {
			return false, fmt.Errorf("unsupported remote computer action %q", p.Action)
		}
		return computerMutates(p.Action), nil
	case "tool_pipeline":
		var p PipelineParams
		if err := json.Unmarshal(req.Input, &p); err != nil {
			return false, err
		}
		if p.Flow != nil {
			return false, fmt.Errorf("remote named browser templates are unavailable; use browser steps or desktop flow")
		}
		for _, step := range p.Steps {
			if step.Tool != BrowserToolName && step.Tool != ComputerToolName {
				return false, fmt.Errorf("remote workflow step %s must use browser or computer", step.ID)
			}
			// Action names cannot be bound dynamically: validate the complete
			// plan before an earlier step can send input.
			if _, exists := step.Bindings["action"]; exists {
				return false, fmt.Errorf("remote workflow action cannot be bound dynamically")
			}
			action, _ := step.Arguments["action"].(string)
			if err := supportActionAllowed(step.Tool, action); err != nil {
				return false, err
			}
		}
		if p.Browser != nil {
			if _, err := compileBrowserWorkflow(*p.Browser); err != nil {
				return false, err
			}
			for _, step := range p.Browser.Steps {
				if err := supportActionAllowed(BrowserToolName, step.Action); err != nil {
					return false, err
				}
			}
		}
		return pipelineMutates(string(req.Input)), nil
	default:
		return false, fmt.Errorf("remote tool must be browser, computer or tool_pipeline")
	}
}

func supportActionAllowed(tool, action string) error {
	if tool == BrowserToolName && slices.Contains(supportBrowserActions, action) {
		return nil
	}
	if tool == ComputerToolName && slices.Contains(computerActions, action) && action != "batch" && action != "handoff" && action != "auth_code" {
		return nil
	}
	return fmt.Errorf("unsupported remote %s action %q", tool, action)
}

func (e *supportExecutor) execution(g nodes.Grant) (*supportExecution, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed[g.SessionID] {
		return nil, fmt.Errorf("session_revoked: this remote execution ended")
	}
	if existing := e.sessions[g.SessionID]; existing != nil {
		return existing, nil
	}
	root := e.stateDir
	// Runtime state belongs to Atlas, not to an exposed drive that may
	// require administrator rights or be shared read-only.
	ctx, cancel := context.WithCancel(context.Background())
	permissions := permission.NewPermissionService(root, true, nil)
	disabled := false
	computerTool := NewComputerTool(permissions, root, config.ToolComputer{Overlay: &disabled}, e.backend, func() bool { return e.backend != nil })
	state := &supportExecution{gate: make(chan struct{}, 1), ctx: ctx, cancel: cancel, computer: computerTool, browser: newBrowserTool(permissions, root, e.manager, browserDescription(false))}
	e.sessions[g.SessionID] = state
	return state, nil
}

