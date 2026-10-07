//go:build windows

package computer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExplorerShellLocationUsesExactWindowAndFilesystemFolder(t *testing.T) {
	start := strings.Index(automationScript, "function Get-ExplorerLocation")
	end := strings.Index(automationScript[start:], "if ($p.action -eq 'windows')") + start
	require.Greater(t, end, start)
	fixture := `
function Get-Process { param($Id,$ErrorAction) [pscustomobject]@{ProcessName='explorer'} }
$root = [pscustomobject]@{Current=[pscustomobject]@{ClassName='CabinetWClass';NativeWindowHandle=42;ProcessId=7}}
function Candidate($id,$url,$path,$fileSystem) {
    [pscustomobject]@{HWND=$id;LocationURL=$url;Document=[pscustomobject]@{Folder=[pscustomobject]@{Self=[pscustomobject]@{Path=$path;IsFileSystem=$fileSystem;IsFolder=$true}}}}
}

$valid = Candidate 42 'file:///C:/Users/%C3%96mer%26Ceylin/deneme' 'C:\Users\Ömer&Ceylin\deneme' $true
$wrong = Candidate 99 'file:///C:/other' 'C:\other' $true
$virtual = Candidate 42 'shell:Downloads' 'Downloads' $false
$mismatch = Candidate 42 'file:///C:/other' 'relative folder label' $true
`
	output := runMockedAutomationScript(t, fixture+automationScript[start:end]+`
$results = @(
    (Get-ExplorerLocation $root @($wrong,$valid)),
    (Get-ExplorerLocation $root @($wrong)),
    (Get-ExplorerLocation $root @($virtual)),
    (Get-ExplorerLocation $root @($mismatch)),
    (Get-ExplorerLocation $root @($valid,$valid))
)
ConvertTo-Json -InputObject $results -Depth 8 -Compress
`, "")
	var got []map[string]any
	require.NoError(t, json.Unmarshal(output, &got), string(output))
	require.Len(t, got, 5)
	require.Equal(t, `C:\Users\Ömer&Ceylin\deneme`, got[0]["path"])
	require.Equal(t, "42", got[0]["window_id"])
	require.Equal(t, true, got[0]["verified"])
	for _, result := range got[1:] {
		require.Nil(t, result)
	}
}

func TestExplorerCurrentFolderLiveReadDoesNotChangeFocus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	b := &windowsBackend{}
	before := b.ForegroundWindow()
	listed, err := b.listNativeWindows(ctx)
	require.NoError(t, err)
	var windows []nativeWindowObservation
	require.NoError(t, json.Unmarshal(listed, &windows))
	for _, w := range windows {
		if w.ClassName != "CabinetWClass" {
			continue
		}
		data, err := b.Automation(ctx, AutomationRequest{Action: "inspect", WindowID: w.ID, MaxElements: 10})
		require.NoError(t, err)
		var o struct {
			Location *struct {
				Path     string `json:"path"`
				ID       string `json:"window_id"`
				Verified bool   `json:"verified"`
			} `json:"explorer_location"`
		}
		require.NoError(t, json.Unmarshal(data, &o))
		if o.Location == nil {
			continue
		}
		require.Equal(t, w.ID, o.Location.ID)
		require.True(t, o.Location.Verified)
		require.NotEmpty(t, o.Location.Path)
		require.Equal(t, before, b.ForegroundWindow(), "Read must not activate the Explorer window")
		t.Log("Exact shell folder returned without changing foreground")
		return
	}
	t.Skip("No readable filesystem Explorer window is currently available")
}
