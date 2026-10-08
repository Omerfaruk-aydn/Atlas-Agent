package tools

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/integrations"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// IntegrationsToolName is the integrations hub tool.
const IntegrationsToolName = "integrations"

//go:embed integrations.md
var integrationsDescription string

// IntegrationsParams are the integrations tool's inputs.
type IntegrationsParams struct {
	Action         string `json:"action" enum:"summary,list,get,check" description:"summary: counts and items that need attention; list: compact rows; get: one item's capabilities, source, last check and next step; check: run the free health check of one item or every item of a kind."`
	ID             string `json:"id,omitempty" description:"get/check: item id such as provider:openai, account:claude, mcp:github, tool:git, dependency:python-stdlib, app:<id>."`
	Kind           string `json:"kind,omitempty" enum:"provider,account,mcp,tool,dependency,app" description:"list/check: limit to one kind."`
	Status         string `json:"status,omitempty" description:"list: one status (ready, unchecked, needs_login, missing_dependency, connection_error, limited, disabled, not_configured) or attention."`
	Query          string `json:"query,omitempty" description:"list: words matched against name, id and capabilities."`
	IncludeCatalog bool   `json:"include_catalog,omitempty" description:"list: also show catalog providers that are not set up."`
}

type integrationsTool struct {
	svc         *integrations.Service
	permissions permission.Service
}

// NewIntegrationsTool lets the agent read the same integration state the
// /integrations screen shows, and run the same free checks.
func NewIntegrationsTool(svc *integrations.Service, permissions permission.Service) fantasy.AgentTool {
	t := &integrationsTool{svc: svc, permissions: permissions}
	return fantasy.NewAgentTool(IntegrationsToolName, integrationsDescription, func(ctx context.Context, _ IntegrationsParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p IntegrationsParams
		if err := strictDecode(call.Input, &p, `{"action":"summary"}`); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(err), nil
		}
		return resp, nil
	})
}

type integrationRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

const maxIntegrationRows = 80

func (t *integrationsTool) run(ctx context.Context, p IntegrationsParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "summary":
		sum := t.svc.Summarize(ctx)
		attention := make([]map[string]any, 0, len(sum.Attention))
		for _, b := range sum.Attention {
			row := map[string]any{"id": b.ID, "name": b.Name, "kind": b.Kind, "status": b.Status}
			putMessage(row, "detail", b.Detail)
			putMessage(row, "next_step", b.Remedy)
			attention = append(attention, row)
		}
		return deviceResponse(map[string]any{
			"ok": true, "total": sum.Total, "by_kind": sum.ByKind, "by_status": sum.ByStatus,
			"unchecked": sum.Unchecked, "attention": attention,
			"note": "Use get for one item's details; statuses other than ready are not usable yet.",
		}), nil
	case "list":
		items := t.svc.Snapshot(ctx, integrations.Filter{Kind: integrations.Kind(p.Kind), Status: p.Status, Query: p.Query, Configured: !p.IncludeCatalog})
		rows := make([]integrationRow, 0, min(len(items), maxIntegrationRows))
		for _, it := range items {
			if len(rows) == maxIntegrationRows {
				break
			}
			rows = append(rows, integrationRow{ID: it.ID, Name: it.Name, Kind: string(it.Kind), Status: string(it.Status), Detail: it.Detail.String()})
		}
		return deviceResponse(map[string]any{"ok": true, "total": len(items), "items": rows}), nil
	case "get":
		if p.ID == "" {
			return fantasy.ToolResponse{}, deviceErr("missing_field", "id", "get needs an id", `{"action":"get","id":"mcp:github"}`)
		}
		it, ok := t.svc.Get(ctx, p.ID)
		if !ok {
			return fantasy.ToolResponse{}, deviceErr("not_found", "id", "no integration "+p.ID, `{"action":"list"}`)
		}
		return deviceResponse(map[string]any{"ok": true, "item": integrationView(it)}), nil
	case "check":
		return t.check(ctx, p, call)
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", "action must be summary, list, get or check", `{"action":"summary"}`)
}

// networkKind reports whether checking kind reaches outside this machine.
func networkKind(k integrations.Kind) bool {
	return k == integrations.KindProvider || k == integrations.KindAccount || k == integrations.KindMCP
}

func (t *integrationsTool) check(ctx context.Context, p IntegrationsParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var ids []string
	switch {
	case p.ID != "":
		ids = []string{p.ID}
	case p.Kind != "":
		ids = t.svc.Checkable(ctx, integrations.Filter{Kind: integrations.Kind(p.Kind), Configured: true})
	default:
		return fantasy.ToolResponse{}, deviceErr("missing_field", "id", "check needs an id or a kind", `{"action":"check","kind":"tool"}`)
	}
	if len(ids) == 0 {
		return deviceResponse(map[string]any{"ok": true, "checked": 0, "note": "Nothing to check."}), nil
	}
	var network []string
	for _, id := range ids {
		it, ok := t.svc.Get(ctx, id)
		if !ok {
			return fantasy.ToolResponse{}, deviceErr("not_found", "id", "no integration "+id, `{"action":"list"}`)
		}
		if networkKind(it.Kind) {
			network = append(network, it.Name)
		}
	}
	if len(network) > 0 && t.permissions != nil {
		desc := fmt.Sprintf("Check %s. Free: lists models or tools and refreshes an expired sign-in; no model or MCP tool is called.", strings.Join(network, ", "))
		ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: IntegrationsToolName, Action: "check", Description: desc, Params: p})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			return fantasy.ToolResponse{}, deviceErr("permission_denied", "", "the user denied the check", "")
		}
	}
	results := t.svc.CheckMany(ctx, ids)
	if errors.Is(ctx.Err(), context.Canceled) {
		return fantasy.ToolResponse{}, deviceErr("canceled", "", "the check was canceled", "")
	}
	out := make([]map[string]any, 0, len(results))
	for _, it := range results {
		row := map[string]any{"id": it.ID, "name": it.Name, "status": it.Status}
		putMessage(row, "detail", it.Detail)
		putMessage(row, "error", it.LastError)
		putMessage(row, "next_step", it.Remedy)
		putMessage(row, "note", it.Stale)
		out = append(out, row)
	}
	return deviceResponse(map[string]any{"ok": true, "checked": len(out), "results": out}), nil
}

func putMessage(row map[string]any, key string, m i18n.Message) {
	if !m.IsZero() {
		row[key] = m.String()
	}
}

// integrationView renders an item for the model in English.
func integrationView(it integrations.Item) map[string]any {
	row := map[string]any{
		"id":     it.ID,
		"name":   it.Name,
		"kind":   it.Kind,
		"status": it.Status,
		"auth":   it.Auth,
		"source": it.Source,
	}
	putMessage(row, "description", it.Description)
	putMessage(row, "detail", it.Detail)
	putMessage(row, "error", it.LastError)
	putMessage(row, "next_step", it.Remedy)
	putMessage(row, "check_cost", it.CheckNote)
	putMessage(row, "note", it.Stale)
	if len(it.Capabilities) > 0 {
		caps := make([]string, len(it.Capabilities))
		for i, c := range it.Capabilities {
			caps[i] = c.String()
		}
		row["capabilities"] = caps
	}
	if len(it.Actions) > 0 {
		row["actions"] = it.Actions
	}
	if !it.CheckedAt.IsZero() {
		row["checked_at"] = it.CheckedAt
	}
	if it.Previous != nil {
		prev := map[string]any{"status": it.Previous.Status, "checked_at": it.Previous.CheckedAt, "meaning": "result from before Atlas restarted; not the current state"}
		putMessage(prev, "detail", it.Previous.Detail)
		row["previous"] = prev
	}
	if it.AppID != "" {
		row["app_id"] = it.AppID
	}
	if it.Live {
		row["live"] = true
	}
	return row
}
