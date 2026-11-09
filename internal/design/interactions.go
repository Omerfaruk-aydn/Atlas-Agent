package design

import (
	"strings"
)

// Interaction is what happens when a person uses a layer in the
// presentation: go to a frame, open an overlay, go back, open a link.
type Interaction struct {
	Trigger string `json:"trigger"`
	Action  string `json:"action"`
	// To is the top-level frame to go to (navigate, overlay) or the layer
	// to scroll into view (scroll_to).
	To         string   `json:"to,omitempty"`
	URL        string   `json:"url,omitempty"`
	Transition string   `json:"transition,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Duration   *float64 `json:"duration,omitempty"`
	Easing     string   `json:"easing,omitempty"`
	Delay      *float64 `json:"delay,omitempty"`
	Key        string   `json:"key,omitempty"`
	Position   string   `json:"position,omitempty"`
}

func sanitizeInteractions(in []*Interaction) []*Interaction {
	var out []*Interaction
	for _, x := range in {
		if x == nil || len(out) == 16 {
			continue
		}
		x.Trigger = oneOf(x.Trigger, "", "click", "hover", "press", "drag", "after", "key")
		x.Action = oneOf(x.Action, "", "navigate", "overlay", "close", "back", "url", "scroll_to")
		if x.Trigger == "" || x.Action == "" {
			continue
		}
		if !validRef(x.To) {
			x.To = ""
		}
		if !strings.HasPrefix(strings.ToLower(x.URL), "http://") && !strings.HasPrefix(strings.ToLower(x.URL), "https://") {
			x.URL = ""
		}
		x.URL = cut(x.URL, 2000)
		if (x.Action == "navigate" || x.Action == "overlay" || x.Action == "scroll_to") && x.To == "" {
			continue
		}
		if x.Action == "url" && x.URL == "" {
			continue
		}
		x.Transition = oneOf(x.Transition, "", "instant", "dissolve", "smart", "slide", "push", "move")
		x.Direction = oneOf(x.Direction, "", "left", "right", "up", "down")
		x.Easing = oneOf(x.Easing, "", "linear", "ease", "ease_in", "ease_out", "ease_in_out")
		x.Position = oneOf(x.Position, "", "center", "top", "bottom", "left", "right")
		x.Key = cut(x.Key, 20)
		if x.Duration != nil {
			v := clamp(*x.Duration, 0, 10000)
			x.Duration = &v
		}
		if x.Delay != nil {
			v := clamp(*x.Delay, 0, 60000)
			x.Delay = &v
		}
		out = append(out, x)
	}
	return out
}
