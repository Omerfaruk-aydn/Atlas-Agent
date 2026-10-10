//go:build !windows

package marking

import (
	"context"
	"errors"
)

// ErrUnsupported is returned off Windows.
var ErrUnsupported = errors.New("screen marking is implemented for Windows desktops only")

// Desktop is unavailable on this platform.
type Desktop struct{}

func (Desktop) Monitors() ([]Monitor, error)     { return nil, ErrUnsupported }
func (Desktop) WindowInfo(uint64) (Window, bool) { return Window{}, false }
func (Desktop) CaptureHash(Rect) (uint64, error) { return 0, ErrUnsupported }

// CaptureMark is unavailable on this platform.
func CaptureMark(context.Context, string) (Mark, []byte, error) { return Mark{}, nil, ErrUnsupported }
