package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/google/uuid"
)

type desktopElement struct {
	Number       int      `json:"element"`
	ID           string   `json:"element_id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	AutomationID string   `json:"automation_id"`
	ProcessID    int      `json:"process_id"`
	Enabled      bool     `json:"enabled"`
	Offscreen    bool     `json:"offscreen"`
	Password     bool     `json:"password"`
	Patterns     []string `json:"supported_patterns"`
	X            float64  `json:"x"`
	Y            float64  `json:"y"`
	Width        float64  `json:"width"`
	Height       float64  `json:"height"`
}
type desktopObservation struct {
	ID        string           `json:"snapshot_id"`
	Created   time.Time        `json:"created_at"`
	WindowID  string           `json:"window_id"`
	Truncated bool             `json:"truncated"`
	Elements  []desktopElement `json:"elements"`
}
type desktopObservations struct {
	mu      sync.Mutex
	entries map[string]desktopObservation
}

func (c *desktopObservations) put(session string, o desktopObservation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]desktopObservation{}
	}
	for id, v := range c.entries {
		if time.Since(v.Created) > 30*time.Second {
			delete(c.entries, id)
		}
	}
	if len(c.entries) >= 64 {
		var oldest string
		var at time.Time
		for id, v := range c.entries {
			if at.IsZero() || v.Created.Before(at) {
				oldest, at = id, v.Created
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[session] = o
}
func (c *desktopObservations) clear() { c.mu.Lock(); defer c.mu.Unlock(); clear(c.entries) }
func (c *desktopObservations) get(session, id string, number int) (desktopObservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	o, ok := c.entries[session]
	if !ok || id == "" || o.ID != id || time.Since(o.Created) > 30*time.Second || number < 1 || number > len(o.Elements) {
		return desktopObservation{}, fmt.Errorf("stale_observation: observe the intended window again")
	}
	return o, nil
}

func desktopMutation(action string) bool {
	switch action {
	case "launch_app", "focus", "invoke", "set_value", "move", "click", "double_click", "right_click", "drag", "scroll", "type", "key", "hotkey":
		return true
	}
	return false
}

func (s *computerToolState) observe(ctx context.Context, p ComputerParams) (fantasy.ToolResponse, error) {
	s.observations.clear()
	driver, ok := s.backend.(computer.AutomationBackend)
	if !ok {
		return fantasy.NewTextErrorResponse("accessibility_unavailable: observation requires accessibility"), nil
	}
	if p.Automation.WindowID == "" {
		return fantasy.NewTextErrorResponse("window_id is required for observe"), nil
	}
	request := p.Automation
	request.Action = "inspect"
	request.ElementID = ""
	request.ImagePath = ""
	if err := computer.ValidateAutomationRequest(request); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	data, err := driver.Automation(ctx, request)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	var o desktopObservation
	if err = json.Unmarshal(data, &o); err != nil || len(o.Elements) > 500 {
		return fantasy.NewTextErrorResponse("Invalid observation response"), nil
	}
	o.ID = uuid.NewString()
	o.Created = time.Now()
	o.WindowID = request.WindowID
	for i := range o.Elements {
		o.Elements[i].Number = i + 1
	}
	response, err := captureWindow(ctx, s.backend, driver, o.WindowID)
	if err != nil || response.IsError {
		return response, err
	}
	// Cache only target descriptors; values and screenshots remain outside the cache.
	s.observations.put(GetSessionFromContext(ctx), o)
	content, _ := json.Marshal(struct {
		desktopObservation
		Crop            string         `json:"crop_description"`
		ScreenOrigin    computer.Point `json:"screen_origin"`
		CoordinateSpace string         `json:"coordinate_space"`
	}{o, response.Content, driver.Origin(), "Control bounds use native desktop pixels; subtract screen_origin for full-screen screenshot coordinates. Crop description gives the crop offset in screenshot coordinates."})
	response.Content = string(content)
	return response, nil
}

func (s *computerToolState) resolveObservation(ctx context.Context, action string, p ComputerParams) (ComputerParams, error) {
	if action != "invoke" && action != "set_value" && action != "assert" {
		return p, fmt.Errorf("invalid_target: numbered references support invoke/set_value/assert")
	}
	o, err := s.observations.get(GetSessionFromContext(ctx), p.SnapshotID, p.Element)
	if err != nil {
		return p, err
	}
	e := o.Elements[p.Element-1]
	if (p.Automation.ElementID != "" && p.Automation.ElementID != e.ID) || (p.Automation.Name != "" && p.Automation.Name != e.Name) || (p.Automation.Role != "" && p.Automation.Role != e.Role) {
		return p, fmt.Errorf("invalid_target: explicit selector conflicts with numbered target")
	}
	if e.ID == "" || e.Password {
		return p, fmt.Errorf("invalid_target: missing runtime identity or password control")
	}
	if p.Automation.WindowID != "" && p.Automation.WindowID != o.WindowID {
		return p, fmt.Errorf("stale_observation: window does not match snapshot")
	}
	driver, ok := s.backend.(computer.AutomationBackend)
	if !ok {
		return p, fmt.Errorf("accessibility_unavailable")
	}
	data, err := driver.Automation(ctx, computer.AutomationRequest{Action: "find", WindowID: o.WindowID, ElementID: e.ID})
	if err != nil {
		return p, err
	}
	var current struct {
		Matches []desktopElement `json:"matches"`
	}
	if json.Unmarshal(data, &current) != nil || len(current.Matches) != 1 {
		return p, fmt.Errorf("stale_observation: target disappeared or became ambiguous")
	}
	n := current.Matches[0]
	if n.ID != e.ID || n.Name != e.Name || n.Role != e.Role || n.ProcessID != e.ProcessID || n.AutomationID != e.AutomationID || n.Password || n.X != e.X || n.Y != e.Y || n.Width != e.Width || n.Height != e.Height || n.Offscreen != e.Offscreen || n.Enabled != e.Enabled {
		return p, fmt.Errorf("stale_observation: target changed; observe again before input")
	}
	p.Automation.WindowID = o.WindowID
	p.Automation.ElementID = e.ID
	p.Automation.Name = e.Name
	p.Automation.Role = e.Role
	return p, nil
}
