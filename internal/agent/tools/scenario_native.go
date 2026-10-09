package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/android"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/scenarios"
)

type nativeElement struct {
	ID           string  `json:"automation_id"`
	Name         string  `json:"name"`
	Text         string  `json:"text"`
	Value        string  `json:"value"`
	Enabled      bool    `json:"enabled"`
	Offscreen    bool    `json:"offscreen"`
	Password     bool    `json:"password"`
	Focused      bool    `json:"keyboard_focused"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	ValuePresent bool    `json:"value_available"`
	TextPresent  bool    `json:"text_available"`
}

type nativeSnapshot struct {
	ObservationID string
	Elements      []nativeElement
	Image         []byte
	Metadata      json.RawMessage
}

// nativeAction dispatches through the coordinator's normal tool invoker so
// discovery, hooks, permissions and cancellation remain authoritative.
func (b *scenarioBackend) nativeAction(ctx context.Context, name string, params any) (fantasy.ToolResponse, error) {
	if b.Invoke == nil {
		return fantasy.ToolResponse{}, fmt.Errorf("native scenario backend unavailable")
	}
	if err := b.Store.CheckOperation(ctx, b.SessionID, b.operationID); err != nil {
		return fantasy.ToolResponse{}, err
	}
	data, err := json.Marshal(params)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	b.index++
	resp, err := b.Invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-native-%d", b.call.ID, b.index), Name: name, Input: string(data)})
	if err != nil {
		return resp, err
	}
	if resp.IsError || resp.StopTurn {
		return resp, fmt.Errorf("native scenario %s failed or needs user input: %s", name, truncateVerification(resp.Content, 2048))
	}
	if len(resp.Content) > 4<<20 {
		return resp, fmt.Errorf("native observation exceeds bounds")
	}
	var status struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal([]byte(resp.Content), &status) == nil && status.OK != nil && !*status.OK {
		return resp, fmt.Errorf("native scenario %s did not succeed: %s", name, truncateVerification(resp.Content, 2048))
	}
	return resp, nil
}

func (b *scenarioBackend) nativeObserve(ctx context.Context, s scenarios.Scenario, image bool) (nativeSnapshot, error) {
	if s.Target == "android" {
		resp, err := b.nativeAction(ctx, AndroidToolName, AndroidParams{Action: "observe", Serial: s.Serial, Image: image})
		if err != nil {
			return nativeSnapshot{}, err
		}
		var observed struct {
			ObservationID string            `json:"observation_id"`
			Serial        string            `json:"serial"`
			Focus         android.Focus     `json:"focus"`
			Elements      []android.Element `json:"elements"`
			Count         int               `json:"element_count"`
		}
		if err := json.Unmarshal([]byte(resp.Content), &observed); err != nil {
			return nativeSnapshot{}, err
		}
		if observed.Serial != s.Serial || observed.Focus.Package != s.Package || observed.ObservationID == "" || observed.Count != len(observed.Elements) {
			return nativeSnapshot{}, fmt.Errorf("android observation has a different identity, foreground package or incomplete element tree")
		}
		snap := nativeSnapshot{ObservationID: observed.ObservationID, Image: resp.Data, Metadata: json.RawMessage(resp.Content)}
		for _, e := range observed.Elements {
			snap.Elements = append(snap.Elements, nativeElement{ID: e.ResourceID, Text: e.Text, Value: e.Text, Name: e.ContentDesc, Enabled: e.Enabled, Focused: e.Focused, Password: e.Password, X: float64(e.Bounds[0]), Y: float64(e.Bounds[1]), Width: float64(e.Bounds[2] - e.Bounds[0]), Height: float64(e.Bounds[3] - e.Bounds[1]), ValuePresent: !e.Password, TextPresent: !e.Password})
		}
		return snap, nil
	}
	resp, err := b.nativeAction(ctx, IsolatedDesktopToolName, IsolatedDesktopParams{Action: "observe", Node: s.Node})
	if err != nil {
		return nativeSnapshot{}, err
	}
	var screen struct {
		ObservationID string `json:"observation_id"`
		Blank         bool   `json:"blank"`
		Windows       []struct {
			ID         string `json:"window_id"`
			Foreground bool   `json:"foreground"`
			Minimized  bool   `json:"minimized"`
		} `json:"windows"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &screen); err != nil {
		return nativeSnapshot{}, err
	}
	if screen.Blank || screen.ObservationID == "" {
		return nativeSnapshot{}, fmt.Errorf("desktop capture is blank or lacks an observation identity")
	}
	foreground := false
	for _, window := range screen.Windows {
		if window.ID == s.WindowID && window.Foreground && !window.Minimized {
			foreground = true
		}
	}
	if !foreground {
		return nativeSnapshot{}, fmt.Errorf("desktop target is not the verified foreground window; bring the guest app forward and observe again")
	}
	inspected, err := b.nativeAction(ctx, IsolatedDesktopToolName, IsolatedDesktopParams{Action: "inspect", Node: s.Node, WindowID: s.WindowID, MaxElements: 500})
	if err != nil {
		return nativeSnapshot{}, err
	}
	var tree struct {
		Result struct {
			WindowID  string          `json:"window_id"`
			Truncated bool            `json:"truncated"`
			Elements  []nativeElement `json:"elements"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(inspected.Content), &tree); err != nil {
		return nativeSnapshot{}, err
	}
	if tree.Result.Truncated || tree.Result.WindowID != s.WindowID {
		return nativeSnapshot{}, fmt.Errorf("desktop accessibility observation is incomplete or belongs to another window")
	}
	return nativeSnapshot{ObservationID: screen.ObservationID, Elements: tree.Result.Elements, Image: resp.Data, Metadata: json.RawMessage(inspected.Content)}, nil
}

func nativeMatch(s scenarios.Scenario, snap nativeSnapshot, selector string) (*nativeElement, error) {
	var matches []nativeElement
	for _, e := range snap.Elements {
		matched := e.ID == selector
		if s.Target == "desktop" {
			matched = strings.HasPrefix(selector, "id:") && e.ID == strings.TrimPrefix(selector, "id:") || strings.HasPrefix(selector, "name:") && e.Name == strings.TrimPrefix(selector, "name:")
		}
		if matched && !e.Offscreen && e.Width > 0 && e.Height > 0 {
			matches = append(matches, e)
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous native selector %q matches %d visible elements", selector, len(matches))
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return &matches[0], nil
}

func (b *scenarioBackend) nativeStep(ctx context.Context, s scenarios.Scenario, step scenarios.Step) error {
	snap, err := b.nativeObserve(ctx, s, false)
	if err != nil {
		return err
	}
	el, err := nativeMatch(s, snap, step.Selector)
	if err != nil {
		return err
	}
	if el == nil || !el.Enabled || el.Password {
		return fmt.Errorf("native input target is missing, disabled or a password field")
	}
	x, y := int(el.X+el.Width/2), int(el.Y+el.Height/2)
	if s.Target == "android" {
		_, err = b.nativeAction(ctx, AndroidToolName, AndroidParams{Action: "tap", Serial: s.Serial, ObservationID: snap.ObservationID, ResourceID: step.Selector})
	} else {
		_, err = b.nativeAction(ctx, IsolatedDesktopToolName, IsolatedDesktopParams{Action: "click", Node: s.Node, WindowID: s.WindowID, ObservationID: snap.ObservationID, X: x, Y: y})
	}
	if err != nil || step.Action == "click" {
		return err
	}
	// Clicking is not proof of focus. Re-read the actual control before any
	// text or key input, including when another dialog intercepted the click.
	snap, err = b.nativeObserve(ctx, s, false)
	if err != nil {
		return err
	}
	el, err = nativeMatch(s, snap, step.Selector)
	if err != nil {
		return err
	}
	if el == nil || !el.Enabled || el.Password || !el.Focused {
		return fmt.Errorf("native target did not acquire keyboard focus; no text or key sent")
	}
	key := step.Value
	keys := map[string]string{"backspace": "delete", "arrowup": "dpad_up", "arrowdown": "dpad_down", "arrowleft": "dpad_left", "arrowright": "dpad_right"}
	if s.Target == "android" {
		if mapped := keys[key]; step.Action == "key" && mapped != "" {
			key = mapped
		}
		_, err = b.nativeAction(ctx, AndroidToolName, AndroidParams{Action: step.Action, Serial: s.Serial, Text: step.Value, Key: key, ObservationID: snap.ObservationID})
	} else {
		keys = map[string]string{"arrowup": "up", "arrowdown": "down", "arrowleft": "left", "arrowright": "right"}
		if mapped := keys[key]; step.Action == "key" && mapped != "" {
			key = mapped
		}
		_, err = b.nativeAction(ctx, IsolatedDesktopToolName, IsolatedDesktopParams{Action: step.Action, Node: s.Node, WindowID: s.WindowID, Text: step.Value, Key: key, ObservationID: snap.ObservationID})
	}
	return err
}

func nativeAssert(s scenarios.Scenario, snap nativeSnapshot, a scenarios.Assertion) (bool, error) {
	el, err := nativeMatch(s, snap, a.Selector)
	if err != nil || el == nil || el.Password {
		return false, err
	}
	switch a.Kind {
	case "visible":
		return true, nil
	case "focused":
		return el.Focused, nil
	case "text":
		return strings.Contains(el.Name, a.Expected) || el.TextPresent && strings.Contains(el.Text, a.Expected), nil
	case "value":
		return el.ValuePresent && el.Value == a.Expected, nil
	}
	return false, fmt.Errorf("unsupported native assertion")
}

func (b *scenarioBackend) native(ctx context.Context, s scenarios.Scenario, run scenarios.ScenarioRun) (scenarios.ScenarioRun, error) {
	run.Status = "failed"
	for i, step := range s.Steps {
		if err := b.nativeStep(ctx, s, step); err != nil {
			return run, fmt.Errorf("native step %d: %w", i+1, err)
		}
	}
	// Assertions retry only observations, never input or a whole scenario.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var snap nativeSnapshot
	passed := false
	for {
		var err error
		snap, err = b.nativeObserve(waitCtx, s, false)
		if err != nil {
			return run, err
		}
		passed = true
		run.Gaps = nil
		for i, a := range s.Assertions {
			ok, err := nativeAssert(s, snap, a)
			if err != nil {
				return run, err
			}
			if !ok {
				passed = false
				run.Gaps = append(run.Gaps, fmt.Sprintf("Assertion failed: %s #%d %s", a.Kind, i+1, a.Selector))
			}
		}
		if passed || waitCtx.Err() != nil {
			break
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if waitCtx.Err() != nil {
			break
		}
	}
	// Bind the final assertions and image to one final observed state.
	var err error
	snap, err = b.nativeObserve(ctx, s, true)
	if err != nil {
		return run, err
	}
	passed = true
	run.Gaps = nil
	for i, a := range s.Assertions {
		ok, err := nativeAssert(s, snap, a)
		if err != nil {
			return run, err
		}
		if !ok {
			passed = false
			run.Gaps = append(run.Gaps, fmt.Sprintf("Assertion failed: %s #%d %s", a.Kind, i+1, a.Selector))
		}
	}
	if len(snap.Image) == 0 || len(snap.Image) > 16<<20 {
		return run, fmt.Errorf("native screenshot is missing or too large")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(snap.Image))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32<<20 {
		return run, fmt.Errorf("native screenshot is not a bounded PNG")
	}
	if _, err := png.Decode(bytes.NewReader(snap.Image)); err != nil {
		return run, err
	}
	for _, capture := range []struct {
		Kind string
		Data []byte
	}{{"scenario-native-tree", snap.Metadata}, {"scenario-screenshot", snap.Image}} {
		ref, err := b.Store.PutArtifact(ctx, capture.Kind, capture.Data)
		if err != nil {
			return run, err
		}
		run.Artifacts = append(run.Artifacts, ref)
	}
	run.Observed, run.Passed = true, passed
	if passed {
		run.Status = "passed"
	}
	return run, nil
}
