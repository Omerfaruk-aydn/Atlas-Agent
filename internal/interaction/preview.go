package interaction

import (
	"bytes"
	"fmt"
	"image/png"
)

// Preview carries sampled colors only; terminal escape sequences are not stored.
type Preview struct {
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Pixels []uint32 `json:"pixels"`
	Path   string   `json:"path"`
}

// Capture stores an explicit private PNG and a bounded TUI thumbnail.
func (c *Controller) Capture(root, id string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > 16*1024*1024 {
		return "", fmt.Errorf("capture must contain 1-16 MiB of PNG data")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 32000000 {
		return "", fmt.Errorf("capture dimensions exceed limit")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	path, err := saveCapture(root, data)
	if err != nil {
		return "", err
	}
	width, height := 60, 24
	pixels := make([]uint32, width*height)
	for y := range height {
		for x := range width {
			r, g, b, _ := img.At(x*config.Width/width, y*config.Height/height).RGBA()
			pixels[y*width+x] = (r>>8)<<16 | (g>>8)<<8 | b>>8
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.Preview = &Preview{Width: width, Height: height, Pixels: pixels, Path: path}
	c.states[id] = s
	return path, nil
}

// SetPreview controls live observation from the user interface.
func (c *Controller) SetPreview(id string, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.states[id]
	s.LivePreview = enabled
	s.ControlRevision++
	c.states[id] = s
}
