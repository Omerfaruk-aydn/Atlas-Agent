//go:build windows

package speech

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpeechSetupCheckOnlyDoesNotInstall(t *testing.T) {
	t.Parallel()
	result, err := Setup(t.Context(), SetupOptions{CheckOnly: true})
	require.NoError(t, err)
	require.Contains(t, []string{"available", "needs_install"}, result.State)
	require.NotEmpty(t, result.Language)
	require.Len(t, result.Capabilities, 2)
	if result.Available {
		require.Contains(t, result.Installed, result.Language)
	}
}

func TestSpeechSetupRejectsUnsupportedLanguageWithoutFallback(t *testing.T) {
	t.Parallel()
	result, err := Setup(t.Context(), SetupOptions{Language: "tr-TR", CheckOnly: true})
	// A separately installed third-party SAPI engine may support Turkish.
	if result.Available {
		require.NoError(t, err)
		require.Equal(t, "tr-TR", result.Language)
		return
	}
	require.ErrorIs(t, err, ErrSetupFailed)
	require.Equal(t, "unsupported_language", result.State)
	require.Empty(t, result.Capabilities)
	require.False(t, result.Fallback)
}

func TestSpeechSetupProtocolDoesNotTreatServicedComponentsAsRecognition(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"requires_admin", "restart_required", "declined", "unavailable", "failed", "install_in_progress", "unsupported_language"} {
		r, err := decodeSetupResult([]byte(`{"state":"`+state+`","available":false,"message":"Not ready"}`), false)
		require.ErrorIs(t, err, ErrSetupFailed, state)
		require.False(t, r.Available)
	}
	_, err := decodeSetupResult([]byte(`{"state":"available","available":false}`), false)
	require.ErrorContains(t, err, "invalid Windows speech setup state")
	_, err = decodeSetupResult([]byte(`{"state":"needs_install","available":false}`), false)
	require.Error(t, err)
	_, err = decodeSetupResult([]byte(`{"state":"needs_install","available":false}`), true)
	require.NoError(t, err)
	_, err = decodeSetupResult([]byte(`{"state":"available","available":true,"language":"en-US","installed":["en-US"]}`), false)
	require.NoError(t, err)
	_, err = decodeSetupResult([]byte("not JSON"), true)
	require.Error(t, err)
}

func TestSpeechSetupCancelledBeforeLaunchingWindowsHelper(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Setup(ctx, SetupOptions{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = Setup(t.Context(), SetupOptions{Language: "en-US;exit"})
	require.Error(t, err)
}

func TestSpeechSetupWindowsServicingWithSimulatedCapabilities(t *testing.T) {
	t.Parallel()
	// Execute the real installation function against simulated DISM commands.
	// No Windows capability is installed, removed or queried by this test.
	const fixture = `
$ErrorActionPreference = 'Stop'
$source = [Console]::In.ReadToEnd() | ConvertFrom-Json
$tokens = $null; $errors = $null
$tree = [Management.Automation.Language.Parser]::ParseInput($source.script, [ref]$tokens, [ref]$errors)
if ($errors.Count) { throw 'Invalid embedded setup syntax' }
$fn = $tree.Find({param($n) $n -is [Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq 'Install-AtlasSpeechCapability'}, $true)
Invoke-Expression $fn.Extent.Text
$body = ${function:Install-AtlasSpeechCapability}.ToString()
$child = "function Install-AtlasSpeechCapability { $body }; exit (Install-AtlasSpeechCapability 'en-US')"
[Management.Automation.Language.Parser]::ParseInput($child, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count) { throw 'Invalid elevated helper syntax' }
function Import-Module { param($Name, $ErrorAction) }
$script:state = 'Installed'; $script:restart = $false; $script:missing = $false; $script:failure = $false
$script:downloads = [Collections.Generic.List[string]]::new()
function Get-WindowsCapability {
    param([switch]$Online, $Name, $ErrorAction)
    if ($script:missing -and $Name -like 'Language.Speech*') { return }
    return [pscustomobject]@{Name = $Name; State = $script:state}
}
function Add-WindowsCapability {
    param([switch]$Online, $Name, $ErrorAction)
    if ($script:failure) { throw 'Simulated Windows Update failure' }
    $script:downloads.Add($Name)
    return [pscustomobject]@{RestartNeeded = $script:restart}
}
$code = Install-AtlasSpeechCapability 'en-US'
if ($code -ne 0 -or $script:downloads.Count -ne 0) { throw 'Existing components must not download' }
$script:state = 'NotPresent'
$code = Install-AtlasSpeechCapability 'en-US'
if ($code -ne 0 -or $script:downloads.Count -ne 2) { throw 'Missing components must download' }
if ($script:downloads[0] -ne 'Language.Basic~~~en-US~0.0.1.0' -or $script:downloads[1] -ne 'Language.Speech~~~en-US~0.0.1.0') { throw 'Wrong capabilities or order' }
$script:restart = $true
if ((Install-AtlasSpeechCapability 'en-US') -ne 3010) { throw 'Restart must be reported' }
$script:missing = $true
if ((Install-AtlasSpeechCapability 'en-US') -ne 9) { throw 'Unavailable capability must fail' }
$script:missing = $false; $script:failure = $true
if ((Install-AtlasSpeechCapability 'en-US') -ne 1) { throw 'Download failure must fail' }
@{ok = $true} | ConvertTo-Json -Compress
`
	data, err := runSpeechHelper(t.Context(), fixture, map[string]string{"script": setupScript})
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(data))
}
