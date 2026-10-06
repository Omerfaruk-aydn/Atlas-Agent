//go:build !windows

package activity

// NewNativeRenderer returns nil when desktop activity is not supported.
func NewNativeRenderer() Renderer { return nil }
