package tools

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

const ComputerToolName = "computer"

//go:embed computer.md.tpl
var computerDescription string

// computerActions lists every value ComputerParams.Action accepts.
var computerActions = []string{
	"screenshot", "screen_size", "cursor_position", "move",
	"click", "double_click", "right_click", "drag",
	"scroll", "type", "key", "hotkey",
}

type ComputerParams struct {
	Action string `json:"action" description:"One of: screenshot, screen_size, cursor_position, move, click, double_click, right_click, drag, scroll, type, key, hotkey. See the tool description for what each needs."`
	// X and Y are screen coordinates in physical pixels, matching the
	// screenshot image 1:1. Required for move, click, double_click,
	// right_click, and drag (start point).
	X int `json:"x,omitempty" description:"Horizontal screen coordinate in pixels, for move, click, double_click, right_click, and drag."`
	Y int `json:"y,omitempty" description:"Vertical screen coordinate in pixels, for move, click, double_click, right_click, and drag."`
	// EndX and EndY are the drag release point.
	EndX int `json:"end_x,omitempty" description:"Horizontal release coordinate in pixels, for drag."`
	EndY int `json:"end_y,omitempty" description:"Vertical release coordinate in pixels, for drag."`
	// Button selects the mouse button for click: left, right, middle.
	Button string `json:"button,omitempty" description:"Mouse button for click: left (default), right, or middle."`
	// Text is typed for the type action.
	Text string `json:"text,omitempty" description:"Text to type as keystrokes into the focused field, for type."`
	// Key is pressed for key and hotkey: a named key (enter, tab, esc,
	// f1-f12, arrows, ...) or a single character.
	Key string `json:"key,omitempty" description:"Key to press, for key and hotkey. Named keys like enter, tab, esc, f5, up, or a single character."`
	// Modifiers joins hotkey modifiers with +: any of ctrl, alt, shift, win.
	Modifiers string `json:"modifiers,omitempty" description:"Hotkey modifiers joined with +, e.g. ctrl or ctrl+shift, for hotkey."`
	// ScrollX and ScrollY are wheel notches for scroll. Positive
	// scroll_y scrolls up.
	ScrollX int `json:"scroll_x,omitempty" description:"Horizontal wheel notches, for scroll."`
	ScrollY int `json:"scroll_y,omitempty" description:"Vertical wheel notches (positive scrolls up), for scroll. Defaults to 3."`
	// FullRes skips the default screenshot downscaling and returns the
	// capture at native resolution. Only needed for pixel-exact
	// inspection; coordinates are always screen pixels either way.
	FullRes bool `json:"full_res,omitempty" description:"For screenshot: return the capture at full native resolution instead of the default downscaled size."`
}

