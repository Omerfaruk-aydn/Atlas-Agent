package dots

import (
	"context"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
)

// screenBackend drives a dot's private browser. It never calls the
// Windows desktop backend, so a dot cannot move the owner's pointer.
type screenBackend struct {
	workspace string
}

// NewScreenBackend returns the computer backend for a dot workspace.
func NewScreenBackend(workspace string) computer.Backend {
	return screenBackend{workspace: workspace}
}

func (b screenBackend) ScreenSize() (computer.Size, error) {
	ep, err := readEndpoint(b.workspace)
	if err != nil || !endpointAlive(ep) {
		return computer.Size{Width: ScreenWidth, Height: ScreenHeight}, nil
	}
	return computer.Size{Width: ep.Width, Height: ep.Height}, nil
}

func (b screenBackend) Screenshot() ([]byte, error) {
	return Capture(context.Background(), b.workspace)
}

func (b screenBackend) CursorPosition() (computer.Point, error) {
	return computer.Point{}, nil
}

func (b screenBackend) MoveTo(x, y int) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "move", X: x, Y: y}, false)
}

func (b screenBackend) Click(x, y int, button computer.MouseButton) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "click", X: x, Y: y, Button: string(button)}, false)
}

func (b screenBackend) DoubleClick(x, y int) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "double_click", X: x, Y: y}, false)
}

func (b screenBackend) Drag(x, y, endX, endY int) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "drag", X: x, Y: y, DX: endX, DY: endY}, false)
}

func (b screenBackend) Scroll(dx, dy int) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "scroll", DX: dx, DY: dy}, false)
}

func (b screenBackend) TypeText(text string) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "type", Text: text}, false)
}

func (b screenBackend) KeyPress(key string) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "key", Key: key}, false)
}

func (b screenBackend) Hotkey(modifiers []string, key string) error {
	return Act(context.Background(), b.workspace, Pointer{Kind: "hotkey", Mods: modifiers, Key: key}, false)
}
