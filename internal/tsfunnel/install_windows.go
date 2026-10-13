		return fmt.Errorf("the installation was cancelled; run it again when ready")
	}
	return fmt.Errorf("the Tailscale installer failed with code %d", code)
}

// OpenApp starts the Tailscale tray app, which starts the service when
// the installation allows it. It never changes the service itself.
func OpenApp() error {
	for _, env := range []string{"ProgramFiles", "ProgramW6432"} {
		dir := os.Getenv(env)
		if dir == "" {
			continue
		}
		p := dir + `\Tailscale\tailscale-ipn.exe`
		if _, err := os.Stat(p); err == nil {
			cmd := exec.Command(p) //nolint:noctx // A GUI app that outlives the call.
			if err := cmd.Start(); err != nil {
				return err
			}
			return cmd.Process.Release()
		}
	}
	return ErrNotInstalled
}

// DefaultInstaller returns the production installer.
func DefaultInstaller(step func(InstallStep)) Installer {
	return Installer{Verify: VerifyAuthenticode, Run: RunMSI, Step: step}
}
