package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/google/uuid"
)

type desktopElement struct {
	Number          int      `json:"element"`
	ID              string   `json:"element_id"`
	Name            string   `json:"name"`
	Role            string   `json:"role"`
	AutomationID    string   `json:"automation_id"`
	ProcessID       int      `json:"process_id"`
	Enabled         bool     `json:"enabled"`
	Offscreen       bool     `json:"offscreen"`
	Password        bool     `json:"password"`
	Patterns        []string `json:"supported_patterns"`
	Value           string   `json:"value,omitempty"`
	Text            string   `json:"text,omitempty"`
	ValueAvailable  bool     `json:"value_available"`
	TextAvailable   bool     `json:"text_available"`
	ValueTruncated  bool     `json:"value_truncated,omitempty"`
	TextTruncated   bool     `json:"text_truncated,omitempty"`
	KeyboardFocused bool     `json:"keyboard_focused"`
	X               float64  `json:"x"`
	Y               float64  `json:"y"`
	Width           float64  `json:"width"`
	Height          float64  `json:"height"`
}
type desktopObservation struct {
	ExplorerLocation *DesktopExplorerLocation `json:"explorer_location,omitempty"`
	ID               string                   `json:"snapshot_id"`
	Created          time.Time                `json:"created_at"`
	WindowID         string                   `json:"window_id"`
	Truncated        bool                     `json:"truncated"`
	Elements         []desktopElement         `json:"elements"`
	Focused          *desktopElement          `json:"focused_element,omitempty"`
}
type desktopObservations struct {
	mu      sync.Mutex
	entries map[string]desktopObservation
}

func (c *desktopObservations) put(session string, o desktopObservation) {
	// Focused content is returned live, never retained by the snapshot cache.
	o.Focused = nil
	o.ExplorerLocation = nil
	o.Elements = append([]desktopElement(nil), o.Elements...)
	for i := range o.Elements {
		o.Elements[i].Value, o.Elements[i].Text = "", ""
		o.Elements[i].ValueAvailable, o.Elements[i].TextAvailable = false, false
	}
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
	case "launch_app", "focus", "select", "invoke", "set_value", "move", "click", "double_click", "right_click", "drag", "scroll", "type", "key", "hotkey":
		return true
	}
	return false
}

func (s *computerToolState) observe(ctx context.Context, p ComputerParams) (fantasy.ToolResponse, error) {
	s.observations.clear()
	mode := p.Observation
	if mode == "" {
		mode = "visual"
	}
	if mode != "visual" && mode != "semantic" && mode != "auto" {
		return fantasy.NewTextErrorResponse("invalid observation mode"), nil
	}
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
		if mode == "auto" && ctx.Err() == nil {
			response, captureErr := captureWindow(ctx, s.backend, driver, request.WindowID)
			if captureErr != nil || response.IsError {
				return response, captureErr
			}
			var metadata struct {
				Foreground *desktopWindowInfo `json:"foreground_window,omitempty"`
			}
			_ = json.Unmarshal([]byte(response.Metadata), &metadata)
			content, marshalErr := json.Marshal(map[string]any{"window_id": request.WindowID, "elements": []desktopElement{}, "truncated": true, "accessibility_status": "unavailable", "accessibility_error": err.Error(), "observation_mode": "visual", "semantic_sufficient": false, "crop_description": response.Content, "screen_origin": driver.Origin(), "foreground_window": metadata.Foreground, "target_window": desktopTargetFor(request.WindowID, metadata.Foreground)})
			if marshalErr != nil {
				return fantasy.ToolResponse{}, marshalErr
			}
			response.Content = string(content)
			return response, nil
		}
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	var o desktopObservation
	if err = json.Unmarshal(data, &o); err != nil || len(o.Elements) > 500 {
		return fantasy.NewTextErrorResponse("Invalid observation response"), nil
	}
	if o.WindowID != "" && o.WindowID != request.WindowID {
		return fantasy.NewTextErrorResponse("Invalid observation window identity"), nil
	}
	expanded := false
	if mode == "auto" && o.Truncated && request.MaxElements > 0 && request.MaxElements < 150 {
		// A tiny scan budget can force an unnecessary visual model turn.
		// Expand once, without replaying input or claiming an incomplete tree is complete.
		larger := request
		larger.MaxElements = 150
		fresh, readErr := driver.Automation(ctx, larger)
		if ctx.Err() != nil {
			return fantasy.ToolResponse{}, ctx.Err()
		}
		if readErr == nil {
			var candidate desktopObservation
			if json.Unmarshal(fresh, &candidate) != nil || len(candidate.Elements) > 500 || (candidate.WindowID != "" && candidate.WindowID != request.WindowID) {
				return fantasy.NewTextErrorResponse("Invalid expanded observation response"), nil
			}
			o = candidate
			expanded = true
		}
	}
	o.ID = uuid.NewString()
	o.Created = time.Now()
	o.WindowID = request.WindowID
	if o.Focused != nil {
		e := o.Focused
		e.Number = 0
		if e.Password {
			e.Name, e.Value, e.Text = "[password]", "[password]", "[password]"
			e.ValueAvailable, e.TextAvailable = false, false
		} else {
			e.Value, e.ValueTruncated = boundedDesktopText(e.Value, e.ValueTruncated)
			e.Text, e.TextTruncated = boundedDesktopText(e.Text, e.TextTruncated)
		}
	}
	for i := range o.Elements {
		o.Elements[i].Number = i + 1
		e := &o.Elements[i]
		if e.Password {
			e.Name, e.Value, e.Text = "[password]", "[password]", "[password]"
			e.ValueAvailable, e.TextAvailable = false, false
		} else {
			e.Value, e.ValueTruncated = boundedDesktopText(e.Value, e.ValueTruncated)
			e.Text, e.TextTruncated = boundedDesktopText(e.Text, e.TextTruncated)
		}
	}
	response := fantasy.NewTextResponse("")
	sufficient := false
	var captureMetadata struct {
		Foreground *desktopWindowInfo `json:"foreground_window,omitempty"`
	}
	if mode != "visual" {
		listed, listErr := driver.Automation(ctx, computer.AutomationRequest{Action: "windows"})
		if listErr != nil {
			return fantasy.NewTextErrorResponse(listErr.Error()), nil
		}
		var windows []desktopWindowInfo
		if json.Unmarshal(listed, &windows) != nil {
			return fantasy.NewTextErrorResponse("Invalid window observation"), nil
		}
		found := false
		for _, w := range windows {
			if w.ID == o.WindowID {
				found = true
			}
			if w.Foreground {
				captureMetadata.Foreground = &w
			}
		}
		if !found {
			return fantasy.NewTextErrorResponse("target_missing: window disappeared; inspect windows again"), nil
		}
		sufficient = captureMetadata.Foreground != nil && captureMetadata.Foreground.ID == o.WindowID && desktopSemanticSufficient(o)
	}
	selectedMode := "semantic"
	if mode == "visual" || (mode == "auto" && !sufficient) {
		selectedMode = "visual"
		response, err = captureWindow(ctx, s.backend, driver, o.WindowID)
		if err != nil || response.IsError {
			return response, err
		}
		if response.Metadata != "" {
			_ = json.Unmarshal([]byte(response.Metadata), &captureMetadata)
		}
	}
	// Cache only target descriptors; values and screenshots remain outside the cache.
	o.ExplorerLocation = verifiedDesktopLocation(o.ExplorerLocation, captureMetadata.Foreground, o.WindowID)
	s.observations.put(GetSessionFromContext(ctx), o)
	pathWarning := ""
	for _, e := range o.Elements {
		if !e.Password && (strings.Contains(e.Name, `:\`) || strings.Contains(e.Name, `\\`)) {
			pathWarning = "UIA names are display labels and may omit ampersands or hide extensions. Do not use a display label as an exact filesystem path. Read the address edit Value/Text or resolve the actual directory; do not guess missing characters."
			break
		}
	}
	content, _ := json.Marshal(struct {
		desktopObservation
		Mode            string             `json:"observation_mode"`
		Sufficient      bool               `json:"semantic_sufficient"`
		Crop            string             `json:"crop_description"`
		ScreenOrigin    computer.Point     `json:"screen_origin"`
		CoordinateSpace string             `json:"coordinate_space"`
		Foreground      *desktopWindowInfo `json:"foreground_window,omitempty"`
		PathWarning     string             `json:"display_path_warning,omitempty"`
		Expanded        bool               `json:"inspection_expanded,omitempty"`
		ContentWarning  string             `json:"content_readback_warning,omitempty"`
		Target          desktopTargetState `json:"target_window"`
	}{o, selectedMode, sufficient, response.Content, driver.Origin(), "Control bounds use native desktop pixels; subtract screen_origin for full-screen screenshot coordinates. Crop description gives the crop offset in screenshot coordinates.", captureMetadata.Foreground, pathWarning, expanded, desktopUnreadableContentWarning(o), desktopTargetFor(o.WindowID, captureMetadata.Foreground)})
	response.Content = string(content)
	return response, nil
}

func boundedDesktopText(value string, truncated bool) (string, bool) {
	runes := []rune(value)
	if len(runes) > 4096 {
		return string(runes[:4096]), true
	}
	return value, truncated
}

// Automatic semantic selection requires readable controls in a complete tree.
func desktopSemanticSufficient(o desktopObservation) bool {
	if o.Truncated || len(o.Elements) == 0 {
		return false
	}
	readable := false
	for _, e := range o.Elements {
		if e.Offscreen || e.Password {
			continue
		}
		if e.ValueTruncated || e.TextTruncated {
			return false
		}
		role := strings.TrimPrefix(e.Role, "ControlType.")
		if e.ID != "" && e.Enabled && e.KeyboardFocused && role == "Pane" && e.Name != "" && len([]rune(e.Name)) <= 4096 {
			readable = true
		}
		if e.ID != "" && e.Name != "" && e.Enabled && e.Width > 0 && e.Height > 0 {
			switch role {
			case "Button", "ListItem", "TreeItem", "MenuItem":
				readable = true
			}
		}
		if e.ID != "" && e.Enabled && (e.ValueAvailable || e.TextAvailable) {
			readable = true
		}
	}
	return readable
}

func desktopUnreadableContentWarning(o desktopObservation) string {
	for _, e := range o.Elements {
		if !e.Password && !e.Offscreen && (e.Role == "ControlType.Edit" || e.Role == "Edit" || e.Role == "ControlType.Document" || e.Role == "Document") && !e.ValueAvailable && !e.TextAvailable {
			return "Usable control identities do not prove unreadable field/document content. Require an actual checkpoint or visual inspection when the requested result depends on that content."
		}
	}
	return ""
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
	if s.overlay && n.Width > 0 && n.Height > 0 && !n.Offscreen {
		activity.Default.Target(engineering.GetScope(ctx, GetSessionFromContext(ctx)).SessionID, o.WindowID, int(n.X+n.Width/2), int(n.Y+n.Height/2))
	}
	p.Automation.WindowID = o.WindowID
	p.Automation.ElementID = e.ID
	p.Automation.Name = e.Name
	p.Automation.Role = e.Role
	return p, nil
}
