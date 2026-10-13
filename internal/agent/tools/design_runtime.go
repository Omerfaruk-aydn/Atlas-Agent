package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// DesignServices routes rendering through existing allowed, hooked tools.
type DesignServices struct {
	Invoke      ToolInvoker
	Permissions permission.Service
}

type (
	designDispatchKey struct{}
	designDispatch    struct{ sent bool }
)

type designBrowserFailure struct{ response fantasy.ToolResponse }

func (e *designBrowserFailure) Error() string { return e.response.Content }

func designFailure(err error) (fantasy.ToolResponse, error) {
	var child *designBrowserFailure
	if errors.As(err, &child) {
		return child.response, nil
	}
	return fantasy.NewTextErrorResponse(err.Error()), nil
}

func designPermission(ctx context.Context, p DesignSystemParams, call fantasy.ToolCall, deps DesignServices, root string) error {
	if deps.Invoke == nil {
		return fmt.Errorf("design renderer unavailable: enable the browser tool")
	}
	if deps.Permissions != nil {
		ok, err := deps.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: DesignSystemToolName, Action: p.Action, Description: "Render and inspect a design in the local automation browser", Path: root, Params: p})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("design rendering denied; no browser input sent")
		}
	}
	return nil
}

func designBrowser(ctx context.Context, deps DesignServices, id string, p BrowserParams) (fantasy.ToolResponse, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	r, err := deps.Invoke(ctx, fantasy.ToolCall{Name: BrowserToolName, ID: id, Input: string(data)})
	if journal, _ := ctx.Value(designDispatchKey{}).(*designDispatch); journal != nil && CallMutates(BrowserToolName, string(data)) {
		if native, ok := ResponseOutcome(r); ok {
			journal.sent = journal.sent || native.Dispatch != "none"
		} else {
			journal.sent = journal.sent || err != nil || !r.IsError
		}
	}
	if err == nil && (r.IsError || r.StopTurn) {
		err = &designBrowserFailure{response: r}
	}
	return r, err
}

// serveDesign exposes only the supplied generated pages on loopback. No
// workspace listing, source credentials or unrelated files are served.
func serveDesign(ctx context.Context, pages map[string]string) (string, func(), error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	host := ln.Addr().String()
	srv := &http.Server{ReadHeaderTimeout: 3 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'")
		_, _ = w.Write([]byte(body))
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	return "http://" + host, func() { _ = srv.Close(); <-done }, nil
}

type localDesignPreviewKey struct{}

func withDesignPreview(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, localDesignPreviewKey{}, origin)
}

func isDesignPreviewCall(ctx context.Context, call fantasy.ToolCall) bool {
	origin, _ := ctx.Value(localDesignPreviewKey{}).(string)
	if origin == "" || call.Name != BrowserToolName {
		return false
	}
	var p BrowserParams
	if json.Unmarshal([]byte(call.Input), &p) != nil {
		return false
	}
	switch p.Action {
	case "navigate":
		return strings.HasPrefix(p.URL, origin+"/")
	case "tab_new":
		return strings.HasPrefix(p.Advanced.URL, origin+"/")
	case "tabs", "tab_select", "tab_close":
		return true
	case "screenshot", "snapshot", "console":
		return true
	case "cdp":
		return p.CDPMethod == "Emulation.setDeviceMetricsOverride"
	}
	return false
}

// openDesignTab leaves the previous automation tab and its viewport intact.
// Cleanup uses a bounded context even when the requesting turn was canceled.
func openDesignTab(ctx context.Context, deps DesignServices, id, url string) (func(), error) {
	tabs, err := designBrowser(ctx, deps, id+"-tabs", BrowserParams{Action: "tabs"})
	if err != nil {
		return nil, err
	}
	var previous struct {
		Active string `json:"active_tab"`
	}
	if json.Unmarshal([]byte(tabs.Content), &previous) != nil {
		return nil, fmt.Errorf("cannot identify the previous automation tab")
	}
	created, err := designBrowser(ctx, deps, id+"-new-tab", BrowserParams{Action: "tab_new", Advanced: browser.Request{URL: url, Purpose: "design preview"}})
	if err != nil {
		return nil, err
	}
	var current struct {
		Active string `json:"active_tab"`
	}
	if json.Unmarshal([]byte(created.Content), &current) != nil || current.Active == "" || current.Active == previous.Active {
		return nil, fmt.Errorf("design tab was not independently created")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = designBrowser(cleanup, deps, id+"-close-tab", BrowserParams{Action: "tab_close", Advanced: browser.Request{TabID: current.Active}})
		if previous.Active != "" {
			_, _ = designBrowser(cleanup, deps, id+"-return-tab", BrowserParams{Action: "tab_select", Advanced: browser.Request{TabID: previous.Active}})
		}
	}, nil
}

func storeDesignRuntime(root, rel string, value any) error {
	path, err := transfer.Resolve(root, filepath.FromSlash(rel))
	if err != nil {
		return err
	}
	return taskstate.WriteJSONAtomic(path, value)
}

func writeDesignArtifact(root, rel string, data []byte) error {
	path, err := transfer.Resolve(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return taskstate.WriteFileAtomic(path, data, 0o600)
}
