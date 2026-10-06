//go:build windows

package activity

// NewNativeRenderer creates an independent desktop surface for local browser
// activity. Its lifetime is owned by the caller's activity manager.
func NewNativeRenderer() Renderer { return newPlatformRenderer() }
