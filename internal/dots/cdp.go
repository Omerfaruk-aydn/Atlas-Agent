package dots

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// Capture returns a PNG of the dot's current page. The debugger
// connection is closed afterwards; the page itself stays open.
func Capture(ctx context.Context, workspace string) ([]byte, error) {
	if alive, _, _ := sandboxState(); alive {
		return sandboxPNG(ctx)
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := callPage(ctx, workspace, "Page.captureScreenshot", map[string]string{"format": "png"}, &shot); err != nil {
		return nil, err
	}
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, err
	}
	if len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, errors.New("the screen capture was not a PNG")
	}
	return png, nil
}

// OpenURL navigates the dot's current page.
func OpenURL(ctx context.Context, workspace, rawURL string) error {
	return callPage(ctx, workspace, "Page.navigate", map[string]string{"url": rawURL}, nil)
}

// Act applies one input event. fromOwner is set when the person who took
// the screen sent it; the dot's own tools pass false and are refused
// while the screen is held.
func Act(ctx context.Context, workspace string, p Pointer, fromOwner bool) error {
	if !fromOwner {
		if err := ErrIfHeld(workspace); err != nil {
			return err
		}
	} else if err := ErrIfHeld(workspace); err != nil {
		// The owner may act only while they hold the screen.
	} else {
		return errors.New("take the screen before sending input")
	}
	switch p.Kind {
	case "drag":
		if err := mouse(ctx, workspace, "mousePressed", p.X, p.Y, "left", 1); err != nil {
			return err
		}
		if err := mouse(ctx, workspace, "mouseMoved", p.DX, p.DY, "left", 0); err != nil {
			return err
		}
		return mouse(ctx, workspace, "mouseReleased", p.DX, p.DY, "left", 1)
	case "click":
		return click(ctx, workspace, p.X, p.Y, p.Button, 1)
	case "double_click":
		return click(ctx, workspace, p.X, p.Y, "left", 2)
	case "move":
		return mouse(ctx, workspace, "mouseMoved", p.X, p.Y, "none", 0)
	case "scroll":
		return callPage(ctx, workspace, "Input.dispatchMouseEvent", map[string]any{
			"type": "mouseWheel", "x": p.X, "y": p.Y, "deltaX": p.DX, "deltaY": -p.DY,
		}, nil)
	case "type":
		if p.Text == "" {
			return errors.New("type needs text")
		}
		return callPage(ctx, workspace, "Input.insertText", map[string]string{"text": p.Text}, nil)
	case "key":
		return pressKey(ctx, workspace, p.Key, nil)
	case "hotkey":
		return pressKey(ctx, workspace, p.Key, p.Mods)
	default:
		return fmt.Errorf("unknown input %q", p.Kind)
	}
}

func click(ctx context.Context, workspace string, x, y int, button string, count int) error {
	switch button {
	case "", "left", "right", "middle":
	default:
		return fmt.Errorf("unknown mouse button %q", button)
	}
	if button == "" {
		button = "left"
	}
	if err := mouse(ctx, workspace, "mousePressed", x, y, button, count); err != nil {
		return err
	}
	return mouse(ctx, workspace, "mouseReleased", x, y, button, count)
}

func mouse(ctx context.Context, workspace, kind string, x, y int, button string, count int) error {
	return callPage(ctx, workspace, "Input.dispatchMouseEvent", map[string]any{
		"type": kind, "x": x, "y": y, "button": button, "clickCount": count,
	}, nil)
}

func pressKey(ctx context.Context, workspace, name string, mods []string) error {
	key, text, err := cdpKey(name)
	if err != nil {
		return err
	}
	bits := 0
	for _, m := range mods {
		switch m {
		case "alt":
			bits |= 1
		case "ctrl":
			bits |= 2
		case "win":
			bits |= 4
		case "shift":
			bits |= 8
		default:
			return fmt.Errorf("unknown modifier %q", m)
		}
	}
	down := map[string]any{"type": "keyDown", "key": key, "modifiers": bits}
	if text != "" {
		down["text"] = text
	}
	if err := callPage(ctx, workspace, "Input.dispatchKeyEvent", down, nil); err != nil {
		return err
	}
	return callPage(ctx, workspace, "Input.dispatchKeyEvent", map[string]any{
		"type": "keyUp", "key": key, "modifiers": bits,
	}, nil)
}

func cdpKey(name string) (key, text string, err error) {
	switch name {
	case "enter":
		return "Enter", "\r", nil
	case "tab":
		return "Tab", "", nil
	case "esc", "escape":
		return "Escape", "", nil
	case "space":
		return " ", " ", nil
	case "backspace":
		return "Backspace", "", nil
	case "delete":
		return "Delete", "", nil
	case "up":
		return "ArrowUp", "", nil
	case "down":
		return "ArrowDown", "", nil
	case "left":
		return "ArrowLeft", "", nil
	case "right":
		return "ArrowRight", "", nil
	case "home":
		return "Home", "", nil
	case "end":
		return "End", "", nil
	case "pageup":
		return "PageUp", "", nil
	case "pagedown":
		return "PageDown", "", nil
	default:
		if len([]rune(name)) == 1 {
			return name, name, nil
		}
		return "", "", fmt.Errorf("unknown key %q", name)
	}
}

func callPage(ctx context.Context, workspace, method string, params, out any) error {
	ep, err := Ensure(ctx, workspace)
	if err != nil {
		return err
	}
	page, err := ensurePage(ctx, ep.HTTP)
	if err != nil {
		return err
	}
	if page.WS == "" {
		return errors.New("the page has no debugger address")
	}
	return cdpCall(ctx, page.WS, method, params, out)
}

func cdpCall(ctx context.Context, wsURL, method string, params, out any) error {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to the dot's page: %w", err)
	}
	defer conn.Close()
	msg := map[string]any{"id": 1, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := conn.WriteJSON(msg); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(20 * time.Second)
	}
	_ = conn.SetReadDeadline(deadline)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var env struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) != nil || env.ID != 1 {
			continue
		}
		if env.Error != nil {
			return errors.New(env.Error.Message)
		}
		if out != nil && len(env.Result) > 0 {
			return json.Unmarshal(env.Result, out)
		}
		return nil
	}
}
