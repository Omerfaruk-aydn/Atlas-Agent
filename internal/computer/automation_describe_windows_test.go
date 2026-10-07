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

func TestWindowsDescribeReadsBoundedContentAndIsolatesPatternFailures(t *testing.T) {
	start := strings.Index(automationScript, "function Finite-Number")
	end := strings.Index(automationScript, "if ($p.action -eq 'windows')")
	require.Greater(t, end, start)
	fixture := `
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$p = @{window_id='42'}
function Mock-Element($mode) {
    $e = [pscustomobject]@{ Mode=$mode; Current=[pscustomobject]@{ NativeWindowHandle=42;Name='Editor';ControlType=[pscustomobject]@{ProgrammaticName='ControlType.Edit'};AutomationId='input';ProcessId=5;IsEnabled=$true;IsOffscreen=$false;IsPassword=($mode -eq 'password');HasKeyboardFocus=$true;BoundingRectangle=[pscustomobject]@{X=0;Y=0;Width=10;Height=10} } }
    $e | Add-Member ScriptMethod GetRuntimeId { @(1,2) }
    $e | Add-Member ScriptMethod GetSupportedPatterns { if ($this.Mode -eq 'broken') { throw 'Provider unsupported' }; @() }
    $e | Add-Member ScriptMethod TryGetCurrentPattern {
        param($id,$result)
        if ($this.Mode -eq 'password') { throw 'Password pattern was accessed' }
        if ($this.Mode -eq 'none') { return $false }
        if ($id -eq [System.Windows.Automation.ValuePattern]::Pattern) {
            if ($this.Mode -eq 'broken') { throw 'Value unavailable' }
            $result.Value = [pscustomobject]@{ Current=[pscustomobject]@{Value=$(if($this.Mode -eq 'empty'){''}else{'v' * 5000})} }
        } else {
            $range = [pscustomobject]@{}
            $range | Add-Member ScriptMethod GetText { param($limit) if ($limit -lt 0 -or $limit -gt 4097) { throw 'Unbounded text read' }; 't' * $limit }
            $result.Value = [pscustomobject]@{DocumentRange=$range}
        }
        return $true
    }
    return $e
}



`
	output := runMockedAutomationScript(t, fixture+automationScript[start:end]+`@('normal','empty','none','broken','password') | ForEach-Object { Describe (Mock-Element $_) } | ConvertTo-Json -Depth 8 -Compress`, "")
	var elements []map[string]any
	require.NoError(t, json.Unmarshal(output, &elements), string(output))
	require.Len(t, elements, 5)
	require.Equal(t, strings.Repeat("v", 4096), elements[0]["value"])
	require.Equal(t, strings.Repeat("t", 4096), elements[0]["text"])
	require.Equal(t, true, elements[0]["value_truncated"])
	require.Equal(t, true, elements[0]["text_truncated"])
	require.Equal(t, true, elements[0]["keyboard_focused"])
	require.Equal(t, "", elements[1]["value"])
	require.Equal(t, true, elements[1]["value_available"], "An empty supported value is readable")
	require.Equal(t, false, elements[1]["value_truncated"])
	require.Equal(t, false, elements[2]["value_available"])
	require.Equal(t, false, elements[2]["text_available"])
	require.Equal(t, false, elements[3]["value_available"])
	require.Equal(t, true, elements[3]["text_available"], "A value failure must not suppress the independent text pattern")
	require.Equal(t, "[password]", elements[4]["name"])
	require.Equal(t, "[password]", elements[4]["value"])
	require.Equal(t, "[password]", elements[4]["text"])
	require.Equal(t, false, elements[4]["value_available"])
	require.Equal(t, false, elements[4]["text_available"])
}

func TestWindowsTraversalMarksChangingProviderIncomplete(t *testing.T) {
	start := strings.Index(automationScript, "while ($queue.Count")
	end := start + strings.Index(automationScript[start:], "if ($p.action -eq 'inspect') {")
	require.Greater(t, end, start)
	fixture := `$ErrorActionPreference='Stop';$p=@{};$limit=10;$truncated=$false;$clock=[System.Diagnostics.Stopwatch]::StartNew();$queue=[System.Collections.Generic.Queue[object]]::new();$queue.Enqueue([pscustomobject]@{});$elements=[System.Collections.Generic.List[object]]::new();$walker=[pscustomobject]@{};$walker|Add-Member ScriptMethod GetFirstChild {param($e) throw 'Provider changed during rename'};`
	output := runMockedAutomationScript(t, fixture+automationScript[start:end]+`@{truncated=$truncated;count=$elements.Count}|ConvertTo-Json -Compress`, "")
	var result struct {
		Truncated bool `json:"truncated"`
		Count     int  `json:"count"`
	}
	require.NoError(t, json.Unmarshal(output, &result), string(output))
	require.True(t, result.Truncated)
	require.Equal(t, 1, result.Count)
}

func TestWindowsFocusedObservationRejectsForeignAncestry(t *testing.T) {
	start := strings.Index(automationScript, "function Describe-Focused")
	end := strings.Index(automationScript, "if ($p.action -eq 'windows')")
	require.Greater(t, end, start)
	fixture := `
$ErrorActionPreference='Stop'
function Mock-Focus($id,$parent,$hasFocus) {
    $e=[pscustomobject]@{Id=$id;Parent=$parent;Current=[pscustomobject]@{HasKeyboardFocus=$hasFocus}}
    $e|Add-Member ScriptMethod GetRuntimeId { @($this.Id) }
    return $e
}
$script:reads=0
function Describe($e) { $script:reads++; @{element_id=[string]$e.Id;value='exact address'} }
$root=Mock-Focus 11 $null $false
$owned=Mock-Focus 12 $root $true
$foreign=Mock-Focus 22 $null $true
$unfocused=Mock-Focus 13 $root $false
$cycle=Mock-Focus 14 $null $true
$cycle.Parent=$cycle
$walker=[pscustomobject]@{}
$walker|Add-Member ScriptMethod GetParent { param($e) $e.Parent }
`
	output := runMockedAutomationScript(t, fixture+automationScript[start:end]+`@{owned=(Describe-Focused $root $owned $walker);foreign=(Describe-Focused $root $foreign $walker);unfocused=(Describe-Focused $root $unfocused $walker);cycle=(Describe-Focused $root $cycle $walker);reads=$script:reads}|ConvertTo-Json -Depth 5 -Compress`, "")
	var result struct {
		Owned     map[string]any `json:"owned"`
		Foreign   any            `json:"foreign"`
		Unfocused any            `json:"unfocused"`
		Cycle     any            `json:"cycle"`
		Reads     int            `json:"reads"`
	}
	require.NoError(t, json.Unmarshal(output, &result), string(output))
	require.Equal(t, "12", result.Owned["element_id"])
	require.Nil(t, result.Foreign)
	require.Nil(t, result.Unfocused)
	require.Nil(t, result.Cycle)
	require.Equal(t, 1, result.Reads, "Foreign or unverified control content must not be read")
}
