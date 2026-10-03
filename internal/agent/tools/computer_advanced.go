package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

func isComputerAutomationAction(action string) bool {
	switch action {
	case "windows", "focus", "inspect", "find", "invoke", "set_value", "assert", "monitors", "ocr", "capture_window":
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
	if action == "capture_window" {
		return captureWindow(ctx, s.backend, driver, request.WindowID)
	}
	for {
		var data json.RawMessage
		var err error
		if action == "ocr" {
			data, err = computer.OCR(ctx, s.backend, driver)
		} else {
			data, err = driver.Automation(ctx, request)
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if action != "assert" {
			origin := driver.Origin()
			result, _ := json.Marshal(struct {
				Result          json.RawMessage `json:"result"`
				ScreenOrigin    computer.Point  `json:"screen_origin"`
				CoordinateSpace string          `json:"coordinate_space"`
			}{data, origin, "UIA uses native desktop pixels; OCR uses screenshot pixels. Subtract screen_origin only for UIA coordinates."})
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
