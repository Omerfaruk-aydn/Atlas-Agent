package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// browserScreenshotGuide explains how the screenshot maps to pointer input.
const browserScreenshotGuide = "Numbered boxes mark interactive elements, labeled with their snapshot ref. " +
	"Prefer ref/semantic actions for DOM controls. For canvas content without refs, use pointer_move/pointer_click/pointer_drag " +
	"with advanced.viewport_id and x/y measured in this image's pixels."

// viewportScreenshotResponse returns the screenshot with its pointer grant.
func viewportScreenshotResponse(ctx context.Context, input browser.InputSession) (fantasy.ToolResponse, error) {
	data, vp, err := input.ViewportScreenshot(ctx)
	if err != nil {
		return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
	}
	resp := fantasy.NewImageResponse(data, "image/png")
	if vp.ID == "" {
		resp.Content = browserScreenshotGuide + " No viewport_id was issued because the page moved or is pinch-zoomed during capture; take another screenshot before pointer input."
		return resp, nil
	}
	grant, _ := json.Marshal(struct {
		ViewportID  string  `json:"viewport_id"`
		ImageWidth  int     `json:"image_width"`
		ImageHeight int     `json:"image_height"`
		ImageScale  float64 `json:"image_scale"`
	}{vp.ID, vp.ImageWidth, vp.ImageHeight, vp.Scale})
	resp.Content = browserScreenshotGuide + "\nViewport: " + string(grant)
	return resp, nil
}

// runBrowserInput dispatches one typed pointer or keyboard action. The
// driver sends events in order and reports the confirmed cleanup outcome
// when a sequence fails or is canceled.
func runBrowserInput(ctx context.Context, sess browser.Session, stateID, action string, params BrowserParams) (fantasy.ToolResponse, error) {
	driver, ok := sess.(browser.InputSession)
	if !ok {
		return fantasy.NewTextErrorResponse("Typed pointer and keyboard input is unavailable in this browser driver"), nil
	}
	p := params.Advanced
	p.Action = action
	if p.Key == "" {
		p.Key = params.Key
	}
	if p.Text == "" {
		p.Text = params.Text
	}
	insertMode := ""
	if action == browser.InputInsertText {
		// mode only says whether insert_text may add to existing text; it
		// is not an input parameter of the driver.
		insertMode, p.Mode = strings.ToLower(strings.TrimSpace(p.Mode)), ""
		if insertMode != "" && insertMode != "new" && insertMode != "append" {
			return contractResponse(computer.NewContractError("invalid_input", "advanced.mode", "insert_text mode must be new (default) or append. No input sent", "Use append to add to text an opened editor already holds.", `{"action":"insert_text","text":"Sign in","advanced":{"mode":"append"}}`)), nil
		}
	}
	if err := browser.ValidateInputRequest(p); err != nil {
		return contractResponse(computer.NewContractError("invalid_input", "advanced", err.Error()+". No input sent", "Correct the listed field; pointer coordinates come from the latest browser screenshot.", browserInputExample(action))), nil
	}
	var focus *browser.FocusInfo
	if action == browser.InputInsertText {
		// Text goes wherever focus is. On an unfocused page or a button it
		// is lost or, in editors, read as shortcuts, so it is refused
		// before anything is sent.
		if reporter, ok := sess.(browser.FocusSession); ok {
			info, err := reporter.Focus(ctx)
			// Editors often move focus to their text editor a moment
			// after the click that opened it; wait briefly for that.
			for attempt := 0; err == nil && !info.Editable && attempt < 4; attempt++ {
				select {
				case <-ctx.Done():
					return fantasy.ToolResponse{}, ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
				info, err = reporter.Focus(ctx)
			}
			if err != nil {
				return contractResponse(computer.NewContractError("focus_unknown", "advanced", "insert_text could not confirm which element has focus ("+err.Error()+"). No input sent", "Observe the page, focus the intended text field or enter the editor's text-edit mode, then call insert_text again.", browserInputExample(action))), nil
			}
			if !info.Editable {
				data, _ := json.Marshal(info)
				return contractResponse(computer.NewContractError("focus_not_editable", "advanced", "insert_text needs an editable focused element; focus is "+string(data)+". No input sent", "Focus the target first: click the text field (ref or semantic_click), or on a canvas select the text tool, click the canvas and confirm text-edit mode before inserting. For a property or form value use set_field.", browserInputExample(action))), nil
			}
			if guard := canvasInsertGuard(stateID, info, referenceIdentity(ctx, sess), insertMode); guard != nil {
				return contractResponse(guard), nil
			}
			focus = &info
		}
	}
	result, err := driver.Input(ctx, p)
	if errors.Is(err, browser.ErrStaleViewport) {
		resp := contractResponse(computer.NewContractError("stale_viewport", "advanced.viewport_id", err.Error(), "Take a new browser screenshot and aim from its viewport_id.", `{"action":"screenshot"}`))
		return resp, nil
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fantasy.ToolResponse{}, ctxErr
		}
		data, _ := json.Marshal(result)
		return fantasy.NewTextErrorResponse(fmt.Sprintf("%s failed after %d event(s): %s. %s", action, result.EventsSent, err.Error(), string(data))), nil
	}
	var report map[string]any
	if focus != nil {
		report = recordCanvasInsert(ctx, sess, stateID, *focus, p.Text)
		shown := *focus
		// Text and node key are reported in canvas_text, not twice.
		shown.Text, shown.Key = "", ""
		result.Focus = &shown
	}
	data, _ := json.Marshal(result)
	return fantasy.WithResponseMetadata(withCanvasInsertReport(fantasy.NewTextResponse(string(data)), report), BrowserResponseMetadata{Action: action}), nil
}

func browserInputExample(action string) string {
	switch action {
	case browser.InputPointerDrag:
		return `{"action":"pointer_drag","advanced":{"viewport_id":"<viewport_id from screenshot>","x":100,"y":120,"to_x":300,"to_y":320}}`
	case browser.InputKeyCombo:
		return `{"action":"key_combo","advanced":{"key":"z","modifiers":["ctrl"]}}`
	case browser.InputInsertText:
		return `{"action":"insert_text","text":"Sign in"}`
	}
	return `{"action":"pointer_click","advanced":{"viewport_id":"<viewport_id from screenshot>","x":100,"y":120}}`
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
