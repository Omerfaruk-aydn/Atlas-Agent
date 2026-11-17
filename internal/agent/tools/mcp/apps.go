package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
)

// MCP Apps: a server can attach an interactive page to a tool. The tool
// declares it in its _meta as ui.resourceUri (a ui:// resource holding HTML)
// and may limit who can call it with ui.visibility, a list of "model" and
// "app". A tool that is app-only exists for the page to call, not for the
// model, so the model is never offered it.

// MaxAppHTMLBytes bounds an app page.
const MaxAppHTMLBytes = 512 * 1024

// appUI reads a tool's ui metadata.
func appUI(tool *Tool) (resourceURI string, visibility []string) {
	ui, _ := tool.Meta["ui"].(map[string]any)
	if ui == nil {
		return "", nil
	}
	resourceURI, _ = ui["resourceUri"].(string)
	if list, ok := ui["visibility"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				visibility = append(visibility, s)
			}
		}
	}
	return resourceURI, visibility
}

// appOnly reports whether a tool is callable only from its app page.
func appOnly(tool *Tool) bool {
	_, visibility := appUI(tool)
	if len(visibility) == 0 {
		return false
	}
	for _, v := range visibility {
		if v == "model" {
			return false
		}
	}
	return true
}

// dropAppOnly removes the tools the model must not see.
func dropAppOnly(tools []*Tool) []*Tool {
	out := make([]*Tool, 0, len(tools))
	for _, t := range tools {
		if !appOnly(t) {
			out = append(out, t)
		}
	}
	return out
}

// AppPage is the interactive page a tool offers.
type AppPage struct {
	Server string
	Tool   string
	URI    string
	HTML   string
}

// AppFor loads the page a tool offers, or returns nil when it offers none.
// It needs the mcp_apps option; with it off, no page is ever fetched. Only
// ui:// resources of text/html type are accepted, and the page is size
// limited: it is rendered by the desktop app in a sandboxed frame, never
// trusted as code of this process.
func AppFor(ctx context.Context, cfg *config.ConfigStore, server, toolName string) (*AppPage, error) {
	if opts := cfg.Config().Options; opts == nil || !opts.MCPApps {
		return nil, errors.New("MCP apps are off; set the mcp_apps option to allow them")
	}
	var tool *Tool
	for name, tools := range Tools() {
		if name != server {
			continue
		}
		for _, t := range tools {
			if t.Name == toolName {
				tool = t
			}
		}
	}
	if tool == nil {
		return nil, fmt.Errorf("no tool %q on server %q", toolName, server)
	}
	uri, _ := appUI(tool)
	if uri == "" {
		return nil, nil
	}
	if !strings.HasPrefix(uri, "ui://") {
		return nil, fmt.Errorf("app resource %q is not a ui:// resource", uri)
	}
	contents, err := ReadResource(ctx, cfg, server, uri)
	if err != nil {
		return nil, err
	}
	for _, c := range contents {
		if c.URI != uri {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(c.MIMEType), "text/html") {
			return nil, fmt.Errorf("app resource %q has type %q, not text/html", uri, c.MIMEType)
		}
		if len(c.Text) > MaxAppHTMLBytes {
			return nil, fmt.Errorf("app page is larger than %d bytes", MaxAppHTMLBytes)
		}
		return &AppPage{Server: server, Tool: toolName, URI: uri, HTML: c.Text}, nil
	}
	return nil, fmt.Errorf("server did not return %q", uri)
}

// AppForTool is AppFor for a tool known by the name the model sees,
// mcp_<server>_<tool>. It returns nil when no tool has that name or the tool
// offers no page.
func AppForTool(ctx context.Context, cfg *config.ConfigStore, fullName string) (*AppPage, error) {
	for server, tools := range Tools() {
		for _, t := range tools {
			if "mcp_"+server+"_"+t.Name == fullName {
				return AppFor(ctx, cfg, server, t.Name)
			}
		}
	}
	return nil, nil
}

// maxAppCallInput bounds the arguments a page may send in one call.
const maxAppCallInput = 64 * 1024

// allAppTools holds, per server, the tools that declare the "app" audience.
// They are kept apart from the tools offered to the model, which never
// includes an app-only tool.
var allAppTools = csync.NewMap[string, []*Tool]()

// recordAppTools remembers a server's page-callable tools, minus any the user
// disabled.
func recordAppTools(server string, tools []*Tool, disabled []string) {
	var out []*Tool
	for _, t := range tools {
		if appCallable(t) && !slices.Contains(disabled, t.Name) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		allAppTools.Del(server)
		return
	}
	allAppTools.Set(server, out)
}

// appCallable reports whether a page may call the tool: it must declare the
// "app" audience explicitly. A tool with no visibility list is meant for the
// model, and a page, which is third-party HTML, does not get to use it.
func appCallable(tool *Tool) bool {
	_, visibility := appUI(tool)
	for _, v := range visibility {
		if v == "app" {
			return true
		}
	}
	return false
}

// AppCallResult is what a page gets back from a tool call.
type AppCallResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// CallFromApp runs a tool on behalf of the page of pageTool (named
// mcp_<server>_<tool>). The call goes to that page's own server only, only to
// tools that declare the "app" audience, with bounded arguments, and only
// while the mcp_apps option is on.
func CallFromApp(ctx context.Context, cfg *config.ConfigStore, pageTool, toolName string, arguments map[string]any) (AppCallResult, error) {
	if opts := cfg.Config().Options; opts == nil || !opts.MCPApps {
		return AppCallResult{}, errors.New("MCP apps are off; set the mcp_apps option to allow them")
	}
	var server string
	var found bool
	for name, tools := range Tools() {
		for _, t := range tools {
			if "mcp_"+name+"_"+t.Name == pageTool {
				server, found = name, true
			}
		}
	}
	if !found {
		return AppCallResult{}, fmt.Errorf("no tool %q", pageTool)
	}
	var target *Tool
	appTools, _ := allAppTools.Get(server)
	for _, t := range appTools {
		if t.Name == toolName {
			target = t
		}
	}
	if target == nil {
		return AppCallResult{}, fmt.Errorf("server %q has no tool %q available to pages", server, toolName)
	}
	if !appCallable(target) {
		return AppCallResult{}, fmt.Errorf("tool %q does not declare that pages may call it", toolName)
	}
	input, err := json.Marshal(arguments)
	if err != nil {
		return AppCallResult{}, err
	}
	if len(input) > maxAppCallInput {
		return AppCallResult{}, fmt.Errorf("arguments are larger than %d bytes", maxAppCallInput)
	}
	res, err := RunTool(ctx, cfg, server, toolName, string(input))
	if err != nil {
		return AppCallResult{}, err
	}
	return AppCallResult{Content: res.Content, IsError: res.IsError}, nil
}
