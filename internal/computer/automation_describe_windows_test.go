//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func runMockedAutomationScript(t *testing.T, script, input string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Stdin = strings.NewReader(input)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return output
}

func TestWindowsLocalizedExplorerLaunchUsesInstalledIdentity(t *testing.T) {
	end := strings.Index(automationScript, "if ($p.action -eq 'monitors')")
	require.Positive(t, end)
	fixture := `function Get-StartApps { [pscustomobject]@{Name='Dosya Gezgini';AppID='Microsoft.Windows.Explorer'} }; function Start-Process { param($FilePath,$ArgumentList) if ($ArgumentList[0] -ne 'shell:AppsFolder\Microsoft.Windows.Explorer') { throw 'Unexpected launch target' } }; `
	for _, name := range []string{"File Explorer", "Explorer", "Dosya Gezgini"} {
		t.Run(name, func(t *testing.T) {
			input, err := json.Marshal(map[string]string{"action": "launch_app", "name": name})
			require.NoError(t, err)
			output := runMockedAutomationScript(t, fixture+automationScript[:end]+"} catch { throw }", string(input))
			require.Contains(t, string(output), `"application_id":"Microsoft.Windows.Explorer"`)
			require.Contains(t, string(output), `"launch_requested":true`)
		})
	}
}
