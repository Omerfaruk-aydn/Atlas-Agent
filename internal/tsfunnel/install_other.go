//go:build !windows

package tsfunnel

// OpenApp is only implemented on Windows.
func OpenApp() error { return ErrNotInstalled }

// DefaultInstaller cannot install outside Windows; Install then points to
// the official download page.
func DefaultInstaller(step func(InstallStep)) Installer {
	return Installer{Step: step}
}
