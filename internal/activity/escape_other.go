//go:build !windows

package activity

// WatchEscape has no native global keyboard provider on this platform.
func WatchEscape(func() func()) func() { return nil }
