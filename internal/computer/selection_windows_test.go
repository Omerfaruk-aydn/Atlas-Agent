//go:build windows

package computer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsSelectionRequiresSingleSelectedFocusedIdentity(t *testing.T) {
	start := strings.Index(automationScript, "function Select-VerifiedItem")
	end := strings.Index(automationScript, "function Describe-Focused")
	require.Greater(t, end, start)
	fixture := `
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$p=@{window_id='11'}
$inputCase=[Console]::In.ReadToEnd()|ConvertFrom-Json
$script:mode=$inputCase.mode
$script:selectCalls=0
$element=[pscustomobject]@{Current=[pscustomobject]@{IsPassword=$false;IsEnabled=$true;IsOffscreen=$false;HasKeyboardFocus=$false}}
$element|Add-Member ScriptMethod GetRuntimeId { @(1,2) }
$element|Add-Member ScriptMethod SetFocus { if($script:mode -ne 'focus_denied'){$this.Current.HasKeyboardFocus=$true} }
$other=[pscustomobject]@{}
$other|Add-Member ScriptMethod GetRuntimeId { @(3,4) }
$selectionInfo=[pscustomobject]@{}
$selectionInfo|Add-Member ScriptMethod GetSelection { if($script:mode -eq 'multiple'){ @($element,$other) } elseif($script:mode -eq 'wrong_identity'){ @($other) } else { @($element) } }
$script:selectionPattern=[pscustomobject]@{Current=$selectionInfo}
$container=[pscustomobject]@{}
$container|Add-Member ScriptMethod TryGetCurrentPattern { param($id,$result) $result.Value=$script:selectionPattern;return $true }
$script:itemPattern=[pscustomobject]@{Current=[pscustomobject]@{IsSelected=$false;SelectionContainer=$container}}
$script:itemPattern|Add-Member ScriptMethod Select { $script:selectCalls++;$this.Current.IsSelected=($script:mode -ne 'not_selected') }
$element|Add-Member ScriptMethod TryGetCurrentPattern { param($id,$result) if($script:mode -eq 'unsupported'){return $false};$result.Value=$script:itemPattern;return $true }
`
	for _, mode := range []string{"success", "multiple", "wrong_identity", "focus_denied", "not_selected", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			output := runMockedAutomationScript(t, fixture+automationScript[start:end]+`try { $r=Select-VerifiedItem $element;@{ok=$true;result=$r;calls=$script:selectCalls}|ConvertTo-Json -Depth 6 -Compress } catch { @{ok=$false;calls=$script:selectCalls}|ConvertTo-Json -Compress }`, `{"mode":"`+mode+`"}`)
			var r struct {
				OK     bool `json:"ok"`
				Calls  int  `json:"calls"`
				Result struct {
					Selected bool `json:"selected"`
					Count    int  `json:"selection_count"`
					Focused  bool `json:"keyboard_focused"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(output, &r), string(output))
			require.Equal(t, mode == "success", r.OK)
			if mode == "unsupported" {
				require.Zero(t, r.Calls)
			} else {
				require.Equal(t, 1, r.Calls, "Selection must not replay")
			}
			if r.OK {
				require.True(t, r.Result.Selected)
				require.True(t, r.Result.Focused)
				require.Equal(t, 1, r.Result.Count)
			}
		})
	}
}
