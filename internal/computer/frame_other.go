//go:build !windows

package computer

// CaptureFrame is not available here; see ErrFrameCaptureUnsupported.
func CaptureFrame(maxWidth int) (Frame, error) { return Frame{}, ErrFrameCaptureUnsupported }
