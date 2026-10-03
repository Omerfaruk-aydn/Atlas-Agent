package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
)

// AutomationRequest targets a fresh accessibility element in one window.
type AutomationRequest struct {
	Action    string `json:"action"`
	WindowID  string `json:"window_id,omitempty"`
	ElementID string `json:"element_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text,omitempty"`
	Condition string `json:"condition,omitempty"`
	Expected  string `json:"expected,omitempty"`
	ImagePath string `json:"image_path,omitempty"`
}

// AutomationBackend supplements pixel input with platform accessibility APIs.
type AutomationBackend interface {
	Automation(context.Context, AutomationRequest) (json.RawMessage, error)
	Origin() Point
}

// CropScreenshot returns native pixels and rejects any out-of-bounds rectangle.
func CropScreenshot(data []byte, x, y, width, height int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if x < 0 || y < 0 || width <= 0 || height <= 0 || width > 4096 || height > 4096 || x > img.Bounds().Dx()-width || y > img.Bounds().Dy()-height {
		return nil, fmt.Errorf("invalid capture rectangle")
	}
	rect := image.Rect(x, y, x+width, y+height)
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil, fmt.Errorf("unsupported screenshot image")
	}
	var buf bytes.Buffer
	err = png.Encode(&buf, sub.SubImage(rect))
	return buf.Bytes(), err
}

// OCR captures the current display to an ephemeral private file for Windows OCR.
func OCR(ctx context.Context, backend Backend, driver AutomationBackend) (json.RawMessage, error) {
	data, err := backend.Screenshot()
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "atlas-ocr-*.png")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	defer os.Remove(path)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return driver.Automation(ctx, AutomationRequest{Action: "ocr", ImagePath: path})
}
