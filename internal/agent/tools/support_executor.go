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

