package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
)

func (b *scenarioBackend) browserAction(ctx context.Context, params BrowserParams) (fantasy.ToolResponse, error) {
	if b.Invoke == nil {
		return fantasy.ToolResponse{}, fmt.Errorf("browser scenario backend unavailable")
	}
	if err := b.Store.CheckOperation(ctx, b.SessionID, b.operationID); err != nil {
		return fantasy.ToolResponse{}, err
	}
	b.index++
	data, err := json.Marshal(params)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	response, err := b.Invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-scenario-%d", b.call.ID, b.index), Name: BrowserToolName, Input: string(data)})
	if err == nil && (response.IsError || response.StopTurn) {
		err = fmt.Errorf("browser scenario action %s failed: %s", params.Action, truncateVerification(response.Content, 2048))
	}
	return response, err
}

func (b *scenarioBackend) viewport(ctx context.Context, width, height int) error {
	data, _ := json.Marshal(map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false})
	_, err := b.browserAction(ctx, BrowserParams{Action: "cdp", CDPMethod: "Emulation.setDeviceMetricsOverride", CDPParams: data})
	return err
}

func (b *scenarioBackend) web(ctx context.Context, s scenarios.Scenario, run scenarios.ScenarioRun) (scenarios.ScenarioRun, error) {
	run.Status = "unavailable"
	if b.Invoke == nil {
		return run, fmt.Errorf("browser scenario backend unavailable")
	}
	run.Status = "failed"
	if err := b.viewport(ctx, s.Width, s.Height); err != nil {
		return run, err
	}
	if _, err := b.browserAction(ctx, BrowserParams{Action: "navigate", URL: s.URL}); err != nil {
		return run, err
	}
	for _, step := range s.Steps {
		var err error
		params := BrowserParams{Action: step.Action, Selector: step.Selector}
		switch step.Action {
		case "resize":
			err = b.viewport(ctx, step.Width, step.Height)
		case "reload":
			current, urlErr := b.browserAction(ctx, BrowserParams{Action: "url"})
			if urlErr != nil {
				err = urlErr
			} else {
				_, err = b.browserAction(ctx, BrowserParams{Action: "navigate", URL: current.Content})
			}
		case "navigate":
			params.URL = step.Value
			_, err = b.browserAction(ctx, params)
		case "type":
			params.Text = step.Value
			_, err = b.browserAction(ctx, params)
		case "key":
			if _, ok := scenarioKeys[step.Value]; !ok {
				err = fmt.Errorf("unsupported browser key")
			} else {
				params.Key = step.Value
				_, err = b.browserAction(ctx, params)
			}
		case "click":
			_, err = b.browserAction(ctx, params)
		default:
			err = fmt.Errorf("unsupported browser scenario action")
		}
		if err != nil {
			return run, err
		}
	}
	passed := true
	for _, a := range s.Assertions {
		selector, _ := json.Marshal(a.Selector)
		expected, _ := json.Marshal(a.Expected)
		expression := "false"
		switch a.Kind {
		case "text":
			expression = fmt.Sprintf("e.textContent.includes(%s)", expected)
		case "value":
			expression = fmt.Sprintf("e.value===%s", expected)
		case "visible":
			expression = "e.getClientRects().length>0 && getComputedStyle(e).visibility!=='hidden'"
		case "focused":
			expression = "document.activeElement===e"
		}
		script := fmt.Sprintf("(()=>{const e=document.querySelector(%s);return !!e && (%s);})()", selector, expression)
		response, err := b.browserAction(ctx, BrowserParams{Action: "eval", Script: script})
		if err != nil {
			return run, err
		}
		var ok bool
		if err := json.Unmarshal([]byte(response.Content), &ok); err != nil {
			return run, fmt.Errorf("invalid browser assertion result: %w", err)
		}
		if !ok {
			passed = false
			run.Gaps = append(run.Gaps, "Assertion failed: "+a.Kind)
		}
	}
	for _, capture := range []struct {
		params BrowserParams
		kind   string
	}{{BrowserParams{Action: "snapshot", Full: true}, "scenario-dom"}, {BrowserParams{Action: "console"}, "scenario-console"}, {BrowserParams{Action: "screenshot"}, "scenario-screenshot"}} {
		response, err := b.browserAction(ctx, capture.params)
		if err != nil {
			return run, err
		}
		data := []byte(response.Content)
		if capture.kind == "scenario-screenshot" {
			data = response.Data
			if len(data) == 0 || len(data) > 16*1024*1024 {
				return run, fmt.Errorf("browser screenshot exceeds bounds or is missing")
			}
			config, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
				return run, fmt.Errorf("invalid bounded PNG screenshot")
			}
			if _, err := png.Decode(bytes.NewReader(data)); err != nil {
				return run, fmt.Errorf("invalid PNG screenshot: %w", err)
			}
		}
		if len(data) > 16*1024*1024 {
			return run, fmt.Errorf("browser capture exceeds bounds")
		}
		ref, err := b.Store.PutArtifact(ctx, capture.kind, data)
		if err != nil {
			return run, err
		}
		run.Artifacts = append(run.Artifacts, ref)
	}
	run.Observed = true
	run.Passed = passed
	if passed {
		run.Status = "passed"
	}
	return run, nil
}
