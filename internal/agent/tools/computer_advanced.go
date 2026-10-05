package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func isComputerAutomationAction(action string) bool {
	switch action {
	case "launch_app", "windows", "focus", "inspect", "find", "invoke", "set_value", "assert", "monitors", "ocr", "capture_window":
		return true
	}
	return false
}

func (s *computerToolState) runAutomation(ctx context.Context, action string, p ComputerParams) (fantasy.ToolResponse, error) {
	driver, ok := s.backend.(computer.AutomationBackend)
	if !ok {
		return fantasy.NewTextErrorResponse("Accessibility automation unavailable in this driver"), nil
	}
	request := p.Automation
	request.Action = action
	request.ImagePath = ""
	if err := computer.ValidateAutomationRequest(request); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	started := time.Now()
	if action == "assert" {
		wait := 5 * time.Second
		if request.WaitMS > 0 {
			wait = time.Duration(request.WaitMS) * time.Millisecond
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	if action == "capture_window" {
		return captureWindow(ctx, s.backend, driver, request.WindowID)
	}
	for {
		var data json.RawMessage
		var err error
		if action == "ocr" {
			if p.Width != 0 || p.Height != 0 {
				data, err = computer.OCRRegion(ctx, s.backend, driver, p.X, p.Y, p.Width, p.Height, request.Name)
			} else {
				data, err = computer.OCR(ctx, s.backend, driver, request.Name)
			}
		} else {
			data, err = driver.Automation(ctx, request)
		}
		if err != nil {
			if action == "assert" && ctx.Err() != nil {
				return fantasy.NewTextErrorResponse("condition_timeout: expected desktop state not reached; observe once before retrying"), nil
			}
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if action != "assert" {
			if action == "invoke" && s.overlay && ctx.Err() == nil {
				var confirmed struct {
					ActionSent bool `json:"action_sent"`
				}
				e := activity.Default.Snapshot()
				if json.Unmarshal(data, &confirmed) == nil && confirmed.ActionSent && e.Point && e.WindowID == request.WindowID && e.Session == engineering.GetScope(ctx, GetSessionFromContext(ctx)).SessionID && e.Resource == "desktop" {
					if activity.Default.Pointer(e.ID, "click", e.X, e.Y) {
						activity.Default.Present()
					}
				}
			}
			origin := driver.Origin()
			imageOrigin := computer.Point{}
			if action == "ocr" && (p.Width != 0 || p.Height != 0) {
				imageOrigin = computer.Point{X: p.X, Y: p.Y}
			}
			result, _ := json.Marshal(struct {
				Result          json.RawMessage `json:"result"`
				ScreenOrigin    computer.Point  `json:"screen_origin"`
				CoordinateSpace string          `json:"coordinate_space"`
				ImageOrigin     computer.Point  `json:"image_origin"`
				ElapsedMS       int64           `json:"elapsed_ms"`
			}{data, origin, "UIA uses native desktop pixels: subtract screen_origin. OCR uses image pixels: add image_origin for screenshot coordinates.", imageOrigin, time.Since(started).Milliseconds()})
			return fantasy.NewTextResponse(string(result)), nil
		}
		var result struct {
			Passed bool `json:"passed"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return fantasy.NewTextErrorResponse("Invalid assertion response"), nil
		}
		if result.Passed {
			return fantasy.NewTextResponse(string(data)), nil
		}
		select {
		case <-ctx.Done():
			return fantasy.NewTextErrorResponse("condition_timeout: expected desktop state not reached"), nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func captureWindow(ctx context.Context, backend computer.Backend, driver computer.AutomationBackend, id string) (fantasy.ToolResponse, error) {
	if id == "" {
		return fantasy.NewTextErrorResponse("window_id is required"), nil
	}
	data, err := driver.Automation(ctx, computer.AutomationRequest{Action: "windows"})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	var windows []struct {
		ID     string  `json:"window_id"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := json.Unmarshal(data, &windows); err != nil {
		return fantasy.NewTextErrorResponse("Invalid window observation"), nil
	}
	for _, window := range windows {
		if window.ID != id {
			continue
		}
		origin := driver.Origin()
		size, err := backend.ScreenSize()
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		x := max(0, int(math.Floor(window.X))-origin.X)
		y := max(0, int(math.Floor(window.Y))-origin.Y)
		right := min(size.Width, int(math.Ceil(window.X+window.Width))-origin.X)
		bottom := min(size.Height, int(math.Ceil(window.Y+window.Height))-origin.Y)
		data, err := backend.Screenshot()
		if err == nil {
			data, err = computer.CropScreenshot(data, x, y, right-x, bottom-y)
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		resp := fantasy.NewImageResponse(data, "image/png")
		resp.Content = fmt.Sprintf("Visible window crop; screenshot origin (%d,%d), %dx%d native pixels. Window may be occluded by other windows.", x, y, right-x, bottom-y)
		return resp, nil
	}
	return fantasy.NewTextErrorResponse("target_missing: window disappeared; inspect windows again"), nil
}
