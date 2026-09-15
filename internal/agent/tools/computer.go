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

// runWithTimeout bounds every backend call: a stuck driver must fail
// the action, never the agent loop. The backend has no context
// plumbing (blocking Win32 syscalls), so the call runs on a goroutine
// and the timeout wins the race. An abandoned call still runs to
// completion in the background; SendInput and capture syscalls return
// in milliseconds in practice, so the window is small.
func (s *computerToolState) runWithTimeout(ctx context.Context, action string, params ComputerParams) (fantasy.ToolResponse, error) {
	timeout := s.actionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		resp fantasy.ToolResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := s.runComputerAction(ctx, action, params)
		done <- result{resp: resp, err: err}
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return fantasy.NewTextErrorResponse(
				fmt.Sprintf("computer action %q timed out after %s.", action, timeout),
			), nil
		}
		return fantasy.ToolResponse{}, ctx.Err()
	case r := <-done:
		return r.resp, r.err
	}
}

// validatePoint checks coordinates against the live screen size, so
// out-of-range aims fail loudly instead of landing clamped at the
// screen edge. The size is read fresh on every call: GetSystemMetrics
// costs nanoseconds, and a cache would go stale when displays change
// mid-session. When the size read itself fails, validation falls back
// to the non-negative check rather than bricking the tool.
func (s *computerToolState) validatePoint(x, y int) error {
	size, err := s.backend.ScreenSize()
	if err != nil {
		return computer.ValidatePoint(x, y)
	}
	return computer.ValidatePointIn(size, x, y)
}

// ComputerPermissionsParams exposes stable tool/action fields for
// permission allowlists, mirroring BrowserPermissionsParams.
func ComputerPermissionsParams(params ComputerParams) map[string]any {
	return map[string]any{
		"action": strings.ToLower(strings.TrimSpace(params.Action)),
	}
}

func computerActionDescription(action string, params ComputerParams) string {
	switch action {
	case "screenshot":
		return "Capture the screen"
	case "screen_size":
		return "Read the screen size"
	case "cursor_position":
		return "Read the pointer position"
	case "move":
		return fmt.Sprintf("Move the pointer to (%d, %d)", params.X, params.Y)
	case "click":
		button, _ := computer.ParseButton(params.Button)
		return fmt.Sprintf("Click %s at (%d, %d)", button, params.X, params.Y)
	case "double_click":
		return fmt.Sprintf("Double-click at (%d, %d)", params.X, params.Y)
	case "right_click":
		return fmt.Sprintf("Right-click at (%d, %d)", params.X, params.Y)
	case "drag":
		return fmt.Sprintf("Drag from (%d, %d) to (%d, %d)", params.X, params.Y, params.EndX, params.EndY)
	case "scroll":
		return fmt.Sprintf("Scroll %+d vertical / %+d horizontal notches", params.ScrollY, params.ScrollX)
	case "type":
		return fmt.Sprintf("Type %q", params.Text)
	case "key":
		return fmt.Sprintf("Press %s", params.Key)
	case "hotkey":
		return fmt.Sprintf("Press %s+%s", params.Modifiers, params.Key)
	default:
		return "Control the computer"
	}
}

func (s *computerToolState) runComputerAction(_ context.Context, action string, params ComputerParams) (fantasy.ToolResponse, error) {
	backend := s.backend
	switch action {
	case "screenshot":
		return s.screenshot(params)
	case "screen_size":
		size, err := backend.ScreenSize()
		if err != nil {
			return fantasy.NewTextErrorResponse("screen_size failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Screen is %d x %d pixels.", size.Width, size.Height)), nil
	case "cursor_position":
		pos, err := backend.CursorPosition()
		if err != nil {
			return fantasy.NewTextErrorResponse("cursor_position failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Pointer is at (%d, %d).", pos.X, pos.Y)), nil
	case "move":
		if err := s.validatePoint(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := backend.MoveTo(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse("move failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Pointer moved to (%d, %d).", params.X, params.Y)), nil
	case "click":
		if err := s.validatePoint(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		button, err := computer.ParseButton(params.Button)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := backend.Click(params.X, params.Y, button); err != nil {
			return fantasy.NewTextErrorResponse("click failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Clicked %s at (%d, %d).", button, params.X, params.Y)), nil
	case "double_click":
		if err := s.validatePoint(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := backend.DoubleClick(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse("double_click failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Double-clicked at (%d, %d).", params.X, params.Y)), nil
	case "right_click":
		if err := s.validatePoint(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := backend.Click(params.X, params.Y, computer.ButtonRight); err != nil {
			return fantasy.NewTextErrorResponse("right_click failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Right-clicked at (%d, %d).", params.X, params.Y)), nil
	case "drag":
		if err := s.validatePoint(params.X, params.Y); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := s.validatePoint(params.EndX, params.EndY); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := backend.Drag(params.X, params.Y, params.EndX, params.EndY); err != nil {
			return fantasy.NewTextErrorResponse("drag failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Dragged from (%d, %d) to (%d, %d).", params.X, params.Y, params.EndX, params.EndY)), nil
	case "scroll":
		dy := params.ScrollY
		if params.ScrollX == 0 && dy == 0 {
			dy = 3
		}
		if err := backend.Scroll(params.ScrollX, dy); err != nil {
			return fantasy.NewTextErrorResponse("scroll failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse("Scrolled."), nil
	case "type":
		if params.Text == "" {
			return fantasy.NewTextErrorResponse("type needs text."), nil
		}
		if err := backend.TypeText(params.Text); err != nil {
			return fantasy.NewTextErrorResponse("type failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse("Typed the text."), nil
	case "key":
		key := strings.ToLower(strings.TrimSpace(params.Key))
		if _, ok := computer.ResolveKey(key); !ok {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown key %q.", params.Key)), nil
		}
		if err := backend.KeyPress(key); err != nil {
			return fantasy.NewTextErrorResponse("key failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Pressed %s.", key)), nil
	case "hotkey":
		mods, err := computer.ParseModifiers(splitModifiers(params.Modifiers))
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if len(mods) == 0 {
			return fantasy.NewTextErrorResponse("hotkey needs modifiers, e.g. ctrl or ctrl+shift."), nil
		}
		key := strings.ToLower(strings.TrimSpace(params.Key))
		if _, ok := computer.ResolveKey(key); !ok {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown key %q.", params.Key)), nil
		}
		if err := backend.Hotkey(mods, key); err != nil {
			return fantasy.NewTextErrorResponse("hotkey failed: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(fmt.Sprintf("Pressed %s+%s.", params.Modifiers, key)), nil
	default:
		return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown action %q.", action)), nil
	}
}

// screenshot captures the screen and, unless full_res is set, downscales
// the capture for model consumption. Coordinates stay in screen pixels
// either way; when the image is scaled, the response text states the
// scale factor so the model can convert (screen = image * scale). If
// downscaling itself fails, the original capture is returned instead:
// a post-processing bug must never brick the core workflow.
func (s *computerToolState) screenshot(params ComputerParams) (fantasy.ToolResponse, error) {
	data, err := s.backend.Screenshot()
	if err != nil {
		return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
	}
	if params.FullRes {
		return fantasy.NewImageResponse(data, "image/png"), nil
	}
	scaled, err := computer.DownscaleScreenshot(data, computer.MaxScreenshotWidth)
	if err != nil {
		resp := fantasy.NewImageResponse(data, "image/png")
		resp.Content = "Full-resolution fallback: downscaling failed (" + err.Error() + ")."
		return resp, nil
	}
	resp := fantasy.NewImageResponse(scaled.PNG, "image/png")
	if scaled.Scale > 1 {
		resp.Content = fmt.Sprintf(
			"Screenshot is %d x %d but the screen is %d x %d. "+
				"Multiply image coordinates by %.2f to get screen pixels.",
			scaled.ImageSize.Width, scaled.ImageSize.Height,
			scaled.ScreenSize.Width, scaled.ScreenSize.Height,
			scaled.Scale,
		)
	}
	return resp, nil
}

// splitModifiers splits a "ctrl+shift" style modifier string into names.
func splitModifiers(modifiers string) []string {
	var out []string
	for _, m := range strings.Split(modifiers, "+") {
		m = strings.ToLower(strings.TrimSpace(m))
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}
