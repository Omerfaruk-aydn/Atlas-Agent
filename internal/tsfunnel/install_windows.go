//go:build windows

package tsfunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// powershell runs a fixed script. File paths travel in environment
// variables, never in the script text, so no quoting can be abused.
func powershell(ctx context.Context, script string, env ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s", err, Sanitize(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// VerifyAuthenticode reads the file's Authenticode signature with the
// built-in Get-AuthenticodeSignature cmdlet.
func VerifyAuthenticode(ctx context.Context, path string) (Signature, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	const script = `$s = Get-AuthenticodeSignature -LiteralPath $env:ATLAS_TS_FILE
$subject = ''
if ($s.SignerCertificate) { $subject = $s.SignerCertificate.Subject }
[pscustomobject]@{ Status = $s.Status.ToString(); Subject = $subject } | ConvertTo-Json -Compress`
	out, err := powershell(ctx, script, "ATLAS_TS_FILE="+path)
	if err != nil {
		return Signature{}, err
	}
	var sig Signature
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &sig); err != nil {
		return Signature{}, fmt.Errorf("%w: signature check output", ErrMalformed)
	}
	return sig, nil
}

// RunMSI opens the standard Windows Installer interface for the verified
// package and waits. Windows asks for administrator approval itself.
// Exit code 3010 means success with a restart pending.
func RunMSI(ctx context.Context, path string) error {
	const script = `$p = Start-Process -FilePath "$env:SystemRoot\System32\msiexec.exe" -ArgumentList @('/i', ('"' + $env:ATLAS_TS_FILE + '"')) -Wait -PassThru
$p.ExitCode`
	out, err := powershell(ctx, script, "ATLAS_TS_FILE="+path)
	if err != nil {
		return err
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return fmt.Errorf("%w: installer exit code", ErrMalformed)
	}
	switch code {
	case 0, 3010:
		return nil
	case 1602:
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
