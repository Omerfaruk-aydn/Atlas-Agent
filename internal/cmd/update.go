package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/update"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
	"github.com/spf13/cobra"
)

// updateCmd upgrades the npm-distributed wrapper package to the latest
// version published on the npm registry. It exists so the TUI can offer
// "press U to update" without shelling out to npm from inside the chat
// loop; the wrapper, in turn, downloads and replaces the platform binary
// via its own postinstall step.
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Upgrade Atlas Agent to the latest version",
	Long: `Check the npm registry for a newer version of @atlas-coder/atlas-agent
and install it globally. The postinstall step in the npm wrapper will replace
the platform binary in <npm prefix>/bin with the matching release.

If you installed Atlas Agent via 'go install' instead, run:

  go install github.com/Omerfaruk-aydn/Atlas-Agent@latest`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()

		fmt.Printf("Current version: v%s\n", strings.TrimPrefix(version.Version, "v"))

		info, err := update.Check(ctx, version.Version, update.Default)
		if err != nil {
			return fmt.Errorf("check for updates: %w", err)
		}
		if info.IsDevelopment() {
			fmt.Println("Skipping update check: this appears to be a development build.")
			return nil
		}
		if !info.Available() {
			fmt.Printf("Already on the latest version (v%s).\n", info.Current)
			return nil
		}

		fmt.Printf("Updating v%s → v%s ...\n", info.Current, info.Latest)

		// The wrapper package is what npm distributes; the binary it
		// installs is replaced by the postinstall script, so the user
		// gets a self-contained upgrade without any extra steps.
		// Windows component downloads can take longer than the registry check.
		installCtx, installCancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
		defer installCancel()
		installCmd, err := npmCommand(installCtx, "install", "-g", "@atlas-coder/atlas-agent@latest")
		if err != nil {
			return err
		}
		installCmd.Stdin = os.Stdin
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		installCmd.Env = append(os.Environ(),
			// npm uses the prefix; let the user override via env if they
			// installed to a non-default location.
			"NPM_CONFIG_UPDATE_NOTIFIER=false",
		)
		if runtime.GOOS == "windows" {
			// Resolve npm before renaming the running image. Windows permits
			// renaming a mapped executable but refuses to overwrite it.
			if err := freeRunningBinaryForReplacement(); err != nil {
				fmt.Printf("Warning: could not free the running binary for replacement (%v); update may fail with EPERM.\n", err)
			}
		}
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("npm install failed: %w", err)
		}

		// npm can exit 0 while still leaving the old binary in place: on
		// Windows, replacing an .exe that another process (a running Atlas
		// Agent session, an antivirus scan) still has open fails with
		// EPERM, which npm reports as a non-fatal "npm warn cleanup" and
		// otherwise ignores. Confirm the installed binary actually reports
		// the new version before declaring success, instead of trusting
		// npm's exit code alone.
		if installedVersion, err := installedBinaryVersion(installCtx); err == nil && installedVersion != "" && installedVersion != info.Latest {
			return fmt.Errorf(
				"npm reported success, but the installed binary still reports v%s (expected v%s). "+
					"This usually means Windows couldn't replace the running executable. "+
					"Close every Atlas Agent window/session and run `atlas-agent update` again",
				installedVersion, info.Latest,
			)
		}

		fmt.Printf("\nUpdated to v%s. Restart any running Atlas Agent sessions.\n", info.Latest)
		return nil
	},
}

// freeRunningBinaryForReplacement renames this process's own executable
// aside (to a ".old" sibling) so its original path is free for npm's
// install step to write the new binary to. Windows allows renaming a file
// that's still mapped into a running process -- the running image keeps
// working from the renamed file -- but never allows overwriting it in
// place. The ".old" file is left behind; it can't be deleted until this
// process exits, and the next update's rename simply replaces it.
func freeRunningBinaryForReplacement() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	old := exe + ".old"
	os.Remove(old) // best-effort cleanup of a previous update's leftover
	return os.Rename(exe, old)
}

// installedBinaryVersion shells out to `npm root -g` to find where the npm
// wrapper installs the platform binary, then runs that binary's own
// --version to see what actually landed on disk -- the only way to catch a
// silently-incomplete replacement (see the EPERM comment above). Returns
// ("", nil) if the binary cannot be located or run, so callers treat that
// as "could not verify" rather than a hard failure: this check is a bonus
// safety net, not the update's success criterion.
func installedBinaryVersion(ctx context.Context) (string, error) {
	rootCmd, err := npmCommand(ctx, "root", "-g")
	if err != nil {
		return "", err
	}
	rootOut, err := rootCmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(rootOut))

	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	name := fmt.Sprintf("atlas-agent-%s-%s", runtime.GOOS, arch)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(root, "@atlas-coder", "atlas-agent", "bin", name)
	if _, err := os.Stat(binPath); err != nil {
		return "", err
	}

	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return "", err
	}
	// Output looks like "atlas-agent-windows-x64 version v0.9.10".
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v"), nil
}

// npmCommand executes npm without passing paths or arguments through a shell.
// Windows distributes npm.cmd; CreateProcess cannot execute that shim directly.
func npmCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return nil, fmt.Errorf("find npm: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(npmPath))
	if runtime.GOOS != "windows" || (ext != ".cmd" && ext != ".bat") {
		return exec.CommandContext(ctx, npmPath, args...), nil
	}
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("find Node.js for npm: %w", err)
	}
	cliPath := filepath.Join(filepath.Dir(npmPath), "node_modules", "npm", "bin", "npm-cli.js")
	info, err := os.Stat(cliPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("npm JavaScript entry point is unavailable at %s; repair the Node.js/npm installation or update with npm install -g @atlas-coder/atlas-agent@latest", cliPath)
	}
	return exec.CommandContext(ctx, nodePath, append([]string{cliPath}, args...)...), nil
}
