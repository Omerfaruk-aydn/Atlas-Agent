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

// NewComputerTool builds the computer-use tool. The backend drives the
// desktop; enabled reports the live toggle state so flipping
// /computer-use off mid-session stops the tool even before the next
// run rebuilds the tool list.
func NewComputerTool(
	permissions permission.Service,
	workingDir string,
	cfg config.ToolComputer,
	backend computer.Backend,
	enabled func() bool,
) fantasy.AgentTool {
	return newComputerTool(permissions, workingDir, backend, enabled, computerDescription, cfg.GetActionTimeout())
}

// computerToolState carries the tool's runtime dependencies plus the
// per-action timeout. It is a struct (rather than bare closure
// captures) so validation helpers and the timeout wrapper share one
// receiver.
type computerToolState struct {
	permissions   permission.Service
	workingDir    string
	backend       computer.Backend
	enabled       func() bool
	actionTimeout time.Duration
}

func newComputerTool(
	permissions permission.Service,
	workingDir string,
	backend computer.Backend,
	enabled func() bool,
	description string,
	actionTimeout time.Duration,
) fantasy.AgentTool {
	state := &computerToolState{
		permissions:   permissions,
		workingDir:    workingDir,
		backend:       backend,
		enabled:       enabled,
		actionTimeout: actionTimeout,
	}
	return fantasy.NewAgentTool(
		ComputerToolName,
		description,
		func(ctx context.Context, params ComputerParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			action := strings.ToLower(strings.TrimSpace(params.Action))
			if !slices.Contains(computerActions, action) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown action %q, must be one of: %s", params.Action, strings.Join(computerActions, ", "))), nil
			}

			if state.enabled != nil && !state.enabled() {
				return fantasy.NewTextErrorResponse(
					"computer-use is off. Ask the user to run /computer-use to turn it on, then retry.",
				), nil
			}
			if state.backend == nil {
				return fantasy.NewTextErrorResponse(
					"computer-use is not available on this machine: " + computer.ErrUnsupportedPlatform.Error(),
				), nil
			}

			sessionID := GetSessionFromContext(ctx)

			// Every computer action runs Safe. Enabling /computer-use is
			// the consent gate — flipping it on means the user accepts
			// unattended screen control — so Manual and AutoAcceptEdits
			// grant immediately instead of prompting per click. Plan
			// mode still denies the tool outright: its category is
			// Execute, and Safe never overrides plan mode.
			p, err := state.permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        state.workingDir,
					ToolCallID:  call.ID,
					ToolName:    ComputerToolName,
					Action:      action,
					Description: computerActionDescription(action, params),
					Params:      ComputerPermissionsParams(params),
					Safe:        true,
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return NewPermissionDeniedResponse(state.permissions), nil
			}

			return state.runWithTimeout(ctx, action, params)
		},
	)
}

