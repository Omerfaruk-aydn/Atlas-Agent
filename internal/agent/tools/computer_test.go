package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

// errComputerTestBoom simulates a backend failure in tests.
var errComputerTestBoom = errors.New("boom")

// fakeComputerBackend records desktop calls so these tests exercise the
// tool's dispatch/validation/permission logic without a real screen.
type fakeComputerBackend struct {
	size            computer.Size
	sizeErr         error
	screenshot      []byte
	screenshotDelay time.Duration
	cursor          computer.Point

	moved    []computer.Point
	clicked  []computer.Point
	clickBtn []computer.MouseButton
	dragged  [][4]int
	scrolled [][2]int
	typed    []string
	keys     []string
	hotkeys  [][2]string
}

func (f *fakeComputerBackend) ScreenSize() (computer.Size, error) {
	if f.sizeErr != nil {
		return computer.Size{}, f.sizeErr
	}
	return f.size, nil
}

func (f *fakeComputerBackend) Screenshot() ([]byte, error) {
	if f.screenshotDelay > 0 {
		time.Sleep(f.screenshotDelay)
	}
	return f.screenshot, nil
}

func (f *fakeComputerBackend) CursorPosition() (computer.Point, error) {
	return f.cursor, nil
}

func (f *fakeComputerBackend) MoveTo(x, y int) error {
	f.moved = append(f.moved, computer.Point{X: x, Y: y})
	return nil
}

func (f *fakeComputerBackend) Click(x, y int, button computer.MouseButton) error {
	f.clicked = append(f.clicked, computer.Point{X: x, Y: y})
	f.clickBtn = append(f.clickBtn, button)
	return nil
}

func (f *fakeComputerBackend) DoubleClick(x, y int) error {
	f.clicked = append(f.clicked, computer.Point{X: x, Y: y})
	return nil
}

func (f *fakeComputerBackend) Drag(x, y, endX, endY int) error {
	f.dragged = append(f.dragged, [4]int{x, y, endX, endY})
	return nil
}

func (f *fakeComputerBackend) Scroll(dx, dy int) error {
	f.scrolled = append(f.scrolled, [2]int{dx, dy})
	return nil
}

func (f *fakeComputerBackend) TypeText(text string) error {
	f.typed = append(f.typed, text)
	return nil
}

func (f *fakeComputerBackend) KeyPress(key string) error {
	f.keys = append(f.keys, key)
	return nil
}

func (f *fakeComputerBackend) Hotkey(modifiers []string, key string) error {
	f.hotkeys = append(f.hotkeys, [2]string{modifiers[0], key})
	return nil
}

func runComputerTool(
	t *testing.T,
	backend *fakeComputerBackend,
	perms permission.Service,
	enabled bool,
	params ComputerParams,
) fantasy.ToolResponse {
	t.Helper()
	return runComputerToolWithTimeout(t, backend, perms, enabled, params, 30*time.Second)
}

func runComputerToolWithTimeout(
	t *testing.T,
	backend *fakeComputerBackend,
	perms permission.Service,
	enabled bool,
	params ComputerParams,
	timeout time.Duration,
) fantasy.ToolResponse {
	t.Helper()

	// A zero screen means "not configured", not "0x0 pixels": give the
	// fake a roomy display so coordinate tests aim at something real
	// unless they set an explicit size.
	if backend.size == (computer.Size{}) && backend.sizeErr == nil {
		backend.size = computer.Size{Width: 4096, Height: 2160}
	}

	input, err := json.Marshal(params)
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	tool := newComputerTool(
		perms,
		t.TempDir(),
		backend,
		func() bool { return enabled },
		"computer",
		timeout,
	)
	resp, err := tool.Run(ctx, fantasy.ToolCall{
		ID:    "test-call",
		Name:  ComputerToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	return resp
}

// testPNG encodes a solid WxH image for screenshot tests.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func decodePNGSize(t *testing.T, data []byte) computer.Size {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	return computer.Size{Width: cfg.Width, Height: cfg.Height}
}

func TestComputerToolRejectsUnknownAction(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	resp := runComputerTool(t, backend, &mockPermissionService{}, true, ComputerParams{Action: "teleport"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unknown action")
}

func TestComputerToolRefusesWhenDisabled(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	resp := runComputerTool(t, backend, &mockPermissionService{}, false, ComputerParams{Action: "screenshot"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "/computer-use")
	require.Empty(t, backend.moved)
}

func TestComputerToolReportsMissingBackend(t *testing.T) {
	t.Parallel()
	input, err := json.Marshal(ComputerParams{Action: "screenshot"})
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), SessionIDContextKey, "test-session")
	tool := newComputerTool(
		&mockPermissionService{},
		t.TempDir(),
		nil,
		func() bool { return true },
		"computer",
		30*time.Second,
	)
	resp, err := tool.Run(ctx, fantasy.ToolCall{ID: "c", Name: ComputerToolName, Input: string(input)})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not available")
}

func TestComputerToolScreenshotReturnsPNG(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	backend.screenshot = testPNG(t, 800, 600)
	resp := runComputerTool(t, backend, &mockPermissionService{}, true, ComputerParams{Action: "screenshot"})
	require.False(t, resp.IsError)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, "image/png", resp.MediaType)
	require.Equal(t, backend.screenshot, resp.Data)
	require.Empty(t, resp.Content, "a capture within limits passes through with no scale note")
}

func TestComputerToolScreenshotDownscalesWideCaptures(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	backend.screenshot = testPNG(t, 3200, 1800)
	resp := runComputerTool(t, backend, &mockPermissionService{}, true, ComputerParams{Action: "screenshot"})
	require.False(t, resp.IsError)
	require.Equal(t, "image", resp.Type)
	got := decodePNGSize(t, resp.Data)
	require.LessOrEqual(t, got.Width, computer.MaxScreenshotWidth)
	require.Equal(t, 1600, got.Width)
	require.Equal(t, 900, got.Height)
	require.Contains(t, resp.Content, "Multiply image coordinates by 2.00")
	require.Contains(t, resp.Content, "3200 x 1800")
}

func TestComputerToolScreenshotFullResSkipsDownscale(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{}
	backend.screenshot = testPNG(t, 3200, 1800)
	resp := runComputerTool(t, backend, &mockPermissionService{}, true,
		ComputerParams{Action: "screenshot", FullRes: true})
	require.False(t, resp.IsError)
	require.Equal(t, backend.screenshot, resp.Data)
	require.Empty(t, resp.Content)
}

func TestComputerToolScreenshotFallsBackOnBadCapture(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{screenshot: []byte{1, 2, 3}}
	resp := runComputerTool(t, backend, &mockPermissionService{}, true, ComputerParams{Action: "screenshot"})
	require.False(t, resp.IsError, "a downscale failure must not brick screenshots")
	require.Equal(t, []byte{1, 2, 3}, resp.Data)
	require.Contains(t, resp.Content, "downscaling failed")
}

func TestComputerToolRejectsOutOfBoundsCoords(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{size: computer.Size{Width: 1920, Height: 1080}}
	resp := runComputerTool(t, backend, &mockPermissionService{}, true,
		ComputerParams{Action: "click", X: 2500, Y: 100})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "outside the 1920 x 1080 screen")
	require.Empty(t, backend.clicked)
}

func TestComputerToolValidatesWithoutKnownSize(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{sizeErr: errComputerTestBoom}
	resp := runComputerTool(t, backend, &mockPermissionService{}, true,
		ComputerParams{Action: "click", X: 100, Y: 200})
	require.False(t, resp.IsError, "a size-read failure falls back to non-negative validation")
	require.Len(t, backend.clicked, 1)
}

func TestComputerToolActionTimesOut(t *testing.T) {
	t.Parallel()
	backend := &fakeComputerBackend{screenshotDelay: 500 * time.Millisecond}
	resp := runComputerToolWithTimeout(t, backend, &mockPermissionService{}, true,
		ComputerParams{Action: "screenshot"}, 20*time.Millisecond)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "timed out")
}

