package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/marking"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

const ScreenMarkToolName = "screen_mark"

//go:embed screen_mark.md
var screenMarkDescription string

// ScreenMarkParams is flat.
type ScreenMarkParams struct {
	Action string `json:"action" enum:"list,get,validate,delete,request" description:"list recent marks; get one (crop image + metadata); validate it against the live screen; delete it; request asks the user to mark something now."`
	ID     string `json:"id,omitempty" description:"Mark id, e.g. mark-0123456789ab."`
	Note   string `json:"note,omitempty" description:"request: short instruction shown to the user."`
}

type screenMarkTool struct {
	permissions permission.Service
	store       marking.Store
	live        marking.Live
}

// NewScreenMarkTool reads and validates user screen marks.
func NewScreenMarkTool(permissions permission.Service, marksDir string) fantasy.AgentTool {
	t := &screenMarkTool{permissions: permissions, store: marking.Store{Dir: marksDir}, live: marking.Desktop{}}
	return fantasy.NewAgentTool(ScreenMarkToolName, screenMarkDescription, func(ctx context.Context, _ ScreenMarkParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		var p ScreenMarkParams
		if err := strictDecode(call.Input, &p, `{"action":"validate","id":"mark-0123456789ab"}`); err != nil {
			return deviceErrorResponse(err), nil
		}
		resp, err := t.run(ctx, p, call)
		if err != nil {
			return deviceErrorResponse(err), nil
		}
		return resp, nil
	})
}

func (t *screenMarkTool) run(ctx context.Context, p ScreenMarkParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "list":
		marks, err := t.store.List()
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		type row struct {
			ID       string           `json:"id"`
			Created  string           `json:"created"`
			Mode     string           `json:"mode"`
			Note     string           `json:"note,omitempty"`
			Window   string           `json:"window,omitempty"`
			Validity marking.Validity `json:"last_validity"`
		}
		var rows []row
		for i, m := range marks {
			if i >= 20 {
				break
			}
			r := row{ID: m.ID, Created: time.UnixMilli(m.CreatedAt).Format(time.RFC3339), Mode: m.Mode, Note: m.Note, Validity: m.Validity}
			if m.Window != nil {
				r.Window = m.Window.Title
			}
			rows = append(rows, r)
		}
		return deviceResponse(map[string]any{"ok": true, "marks": rows}), nil
	case "get", "validate":
		m, crop, err := t.store.Get(p.ID)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr("not_found", "id", err.Error(), "Call screen_mark list.")
		}
		m.Validity = marking.Validate(m, t.live, time.Now())
		_ = t.store.Update(m)
		meta := map[string]any{"ok": true, "mark": m, "targetable": m.Validity.Targetable()}
		if !m.Validity.Targetable() {
			meta["note"] = "This mark is " + m.Validity.State + ". Do not click or type at its coordinates; ask the user to mark again if the target is still needed."
		}
		if p.Action == "validate" {
			return deviceResponse(meta), nil
		}
		meta["manifest"] = marking.Manifest(m)
		img, scale := crop, 1.0
		if scaled, err := computer.DownscaleScreenshot(crop, computer.MaxScreenshotWidth); err == nil {
			img, scale = scaled.PNG, scaled.Scale
		}
		meta["image_scale"] = scale
		data, _ := json.Marshal(meta)
		resp := fantasy.NewImageResponse(img, "image/png")
		resp.Content = string(data)
		return resp, nil
	case "delete":
		if err := t.store.Delete(p.ID); err != nil {
			return fantasy.ToolResponse{}, err
		}
		return deviceResponse(map[string]any{"ok": true, "deleted": p.ID}), nil
	case "request":
		if t.permissions != nil {
			ok, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), ToolCallID: call.ID, ToolName: ScreenMarkToolName, Action: "request", Description: "Show the screen marking overlay so you can mark a region: " + p.Note, Params: p})
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !ok {
				return fantasy.ToolResponse{}, deviceErr("permission_denied", "", "the user declined to mark", "")
			}
		}
		m, crop, err := marking.CaptureMark(ctx, p.Note)
		if err != nil {
			return fantasy.ToolResponse{}, deviceErr("mark_failed", "", err.Error(), "The user may have canceled with Escape; ask before requesting again.")
		}
		saved, err := t.store.Save(m, crop)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		return t.run(ctx, ScreenMarkParams{Action: "get", ID: saved.ID}, call)
	}
	return fantasy.ToolResponse{}, deviceErr("invalid_action", "action", fmt.Sprintf("unknown action %q", p.Action), "")
}
