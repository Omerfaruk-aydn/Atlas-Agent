//go:build !windows

package computer

// openPlatform reports ErrUnsupportedPlatform: computer-use ships
// Windows-first and no backend exists here yet.
func openPlatform() (Backend, error) {
	return nil, ErrUnsupportedPlatform
}
