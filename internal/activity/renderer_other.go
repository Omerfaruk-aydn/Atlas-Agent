//go:build !windows

package activity

type noRenderer struct{}

func (noRenderer) Render(Event)     {}
func (noRenderer) Close()           {}
func newPlatformRenderer() Renderer { return noRenderer{} }
func IsOverlayWindow(uintptr) bool  { return false }
