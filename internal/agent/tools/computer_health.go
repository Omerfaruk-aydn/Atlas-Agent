package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
)

type desktopHealthCheck struct {
	Code      string `json:"code,omitempty"`
	Status    string `json:"status"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Detail    string `json:"detail,omitempty"`
	Recovery  string `json:"recovery,omitempty"`
}

func (s *computerToolState) health(ctx context.Context, p ComputerParams) (fantasy.ToolResponse, error) {
	checks := map[string]desktopHealthCheck{}
	overall := "ok"
	check := func(name string, fn func() error) {
		start := time.Now()
		err := ctx.Err()
		if err == nil {
			err = fn()
		}
		v := desktopHealthCheck{Status: "ok", ElapsedMS: time.Since(start).Milliseconds()}
		if err != nil {
			overall = "degraded"
			v.Status = "failed"
			v.Detail = err.Error()
			v.Code, v.Recovery = interaction.Failure(err.Error())
		}
		checks[name] = v
	}
	check("screen_size", func() error {
		size, err := s.backend.ScreenSize()
		if err == nil && (size.Width <= 0 || size.Height <= 0) {
			err = fmt.Errorf("invalid screen dimensions")
		}
		return err
	})
	check("capture", func() error {
		data, err := s.backend.Screenshot()
		if err == nil && len(data) == 0 {
			err = fmt.Errorf("empty screenshot")
		}
		return err
	})
	if driver, ok := s.backend.(computer.AutomationBackend); ok {
		check("window_enumeration", func() error {
			_, err := driver.Automation(ctx, computer.AutomationRequest{Action: "windows"})
			return err
		})
		if p.Automation.WindowID != "" {
			check("accessibility", func() error {
				_, err := driver.Automation(ctx, computer.AutomationRequest{Action: "inspect", WindowID: p.Automation.WindowID, MaxElements: 20})
				return err
			})
		} else {
			checks["accessibility"] = desktopHealthCheck{Status: "skipped", Detail: "Supply window_id to test its provider"}
		}
	} else {
		checks["window_enumeration"] = desktopHealthCheck{Status: "skipped"}
		checks["accessibility"] = desktopHealthCheck{Status: "skipped"}
		overall = "degraded"
	}
	if driver, ok := s.backend.(interface{ ForegroundWindow() string }); ok {
		check("foreground", func() error {
			if driver.ForegroundWindow() == "" || driver.ForegroundWindow() == "0" {
				return fmt.Errorf("no foreground window")
			}
			return nil
		})
	} else {
		checks["foreground"] = desktopHealthCheck{Status: "skipped"}
	}
	data, _ := json.Marshal(map[string]any{"status": overall, "checks": checks, "read_only": true})
	return fantasy.NewTextResponse(string(data)), nil
}
