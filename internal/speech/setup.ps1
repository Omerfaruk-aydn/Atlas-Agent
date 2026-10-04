$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$inputData = [Console]::In.ReadToEnd() | ConvertFrom-Json
$result = @{ available = $false; language = ''; installed = @(); state = 'failed'; capabilities = @(); restart_required = $false; fallback = $false; message = '' }

# Install only speech and its basic-language dependency; never change display
# language, keyboard settings, cloud recognition or microphone permissions.
function Install-AtlasSpeechCapability([string]$language) {
    $mutex = $null
    $ownsMutex = $false
    try {
        $mutex = [Threading.Mutex]::new($false, 'Global\AtlasOfflineSpeechSetup')
        try { $ownsMutex = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $ownsMutex = $true }
        if (-not $ownsMutex) { return 1618 }
        Import-Module Dism -ErrorAction Stop
        $restart = $false
        foreach ($feature in @('Basic', 'Speech')) {
            $name = "Language.$feature~~~$language~0.0.1.0"
            $capability = @(Get-WindowsCapability -Online -Name $name -ErrorAction Stop)
            if ($capability.Count -ne 1 -or $capability[0].Name -ne $name) { return 9 }
            if ($capability[0].State -ne 'Installed') {
                $added = Add-WindowsCapability -Online -Name $name -ErrorAction Stop
                if ($added.RestartNeeded) { $restart = $true }
            }
        }
        if ($restart) { return 3010 }
        return 0
    } catch { return 1 } finally {
        if ($ownsMutex) { $mutex.ReleaseMutex() }
        if ($mutex) { $mutex.Dispose() }
    }
}

function Get-AtlasRecognizers {
    Add-Type -AssemblyName System.Speech
    return @([System.Speech.Recognition.SpeechRecognitionEngine]::InstalledRecognizers() | ForEach-Object { $_.Culture.Name })
}

try {
    $supported = @('en-US', 'en-GB', 'fr-FR', 'de-DE', 'es-ES', 'ja-JP', 'zh-CN', 'zh-TW')
    $result.installed = @(Get-AtlasRecognizers)
    $language = [string]$inputData.language
    if ($language) {
        if ($language -notmatch '^[a-z]{2,3}(-[A-Z]{2})?$') { throw 'Invalid language code.' }
        $installedMatch = $result.installed | Where-Object { $_ -eq $language -or $_.Split('-')[0] -eq $language } | Select-Object -First 1
        if ($installedMatch) {
            $language = $installedMatch
        } elseif ($supported -notcontains $language) {
            $language = $supported | Where-Object { $_.Split('-')[0] -eq $language } | Select-Object -First 1
            if (-not $language) {
                $result.state = 'unsupported_language'
                $result.message = "Windows offline dictation does not support '$($inputData.language)'. Turkish is not supported by this engine. No other language was installed."
            }
        }
    } elseif ($result.installed.Count -gt 0) {
        $language = $result.installed | Where-Object { $_ -eq [Globalization.CultureInfo]::CurrentCulture.Name } | Select-Object -First 1
        if (-not $language) { $language = $result.installed[0] }
    } else {
        $preferred = [Globalization.CultureInfo]::CurrentCulture.Name
        if ($supported -contains $preferred) { $language = $preferred } else { $language = 'en-US'; $result.fallback = $true }
    }

    if ($language) {
        $result.language = $language
        $result.capabilities = @("Language.Basic~~~$language~0.0.1.0", "Language.Speech~~~$language~0.0.1.0")
        if ($result.installed -contains $language) {
            $result.available = $true
            $result.state = 'available'
            $result.message = "Offline Windows dictation is ready ($language)."
        } elseif ($inputData.check_only) {
            $result.state = 'needs_install'
            $result.message = "Offline Windows dictation ($language) needs speech components from Windows Update. Installation may require administrator approval."
        } else {
            $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
            $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
            $code = -1
            if ($isAdmin) {
                $code = Install-AtlasSpeechCapability $language
            } elseif (-not $inputData.allow_elevation) {
                $result.state = 'requires_admin'
                $result.message = 'Windows speech installation needs administrator approval. Run atlas-agent voice setup to retry with UAC.'
            } else {
                # The elevated process executes only this embedded installation
                # function. Language is restricted to the exact allow-list.
                if ($supported -notcontains $language) { throw 'Unsupported installation language.' }
                $body = ${function:Install-AtlasSpeechCapability}.ToString()
                $childScript = "function Install-AtlasSpeechCapability { $body }; exit (Install-AtlasSpeechCapability '$language')"
                $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($childScript))
                try {
                    $process = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList @('-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', $encoded) -Verb RunAs -WindowStyle Hidden -PassThru
                    try {
                        if ($process.WaitForExit(900000)) {
                            $code = $process.ExitCode
                        } else {
                            $result.state = 'install_in_progress'
                            $result.message = 'Windows is still installing speech components. Check atlas-agent voice status later; do not start another installation yet.'
                        }
                    } finally { $process.Dispose() }
                } catch {
                    if ($_.Exception.NativeErrorCode -eq 1223 -or $_.Exception.InnerException.NativeErrorCode -eq 1223) {
                        $result.state = 'declined'
                        $result.message = 'Windows administrator approval was declined. Atlas remains installed; microphone dictation is unavailable.'
                    } else {
                        $result.state = 'failed'
                        $result.message = 'Windows could not start speech installation. Retry atlas-agent voice setup from an administrator terminal.'
                    }
                }
            }

            if ($code -ge 0) {
                $result.installed = @(Get-AtlasRecognizers)
                $result.restart_required = ($code -eq 3010)
                if ($code -eq 1618) {
                    $result.state = 'install_in_progress'
                    $result.message = 'Another Atlas speech installation is running. Wait for it to finish, then check atlas-agent voice status.'
                } elseif ($code -ne 0 -and $code -ne 3010) {
                    $result.state = 'failed'
                    $result.message = "Windows speech installation failed (code $code). Check Windows Update, network access and organizational policy, then retry atlas-agent voice setup."
                } elseif ($result.installed -contains $language) {
                    $result.available = $true
                    $result.state = 'available'
                    $result.message = "Offline Windows dictation is ready ($language)."
                } elseif ($result.restart_required) {
                    $result.state = 'restart_required'
                    $result.message = 'Windows requested a restart after speech installation. Restart Windows, then check atlas-agent voice status.'
                } else {
                    $result.state = 'unavailable'
                    $result.message = 'Windows components were serviced, but no compatible offline dictation engine is visible. This Windows build may not provide the legacy engine. Check atlas-agent voice status; component installation alone does not guarantee recognition.'
                }
            }
        }
    }
} catch {
    $result.state = 'failed'
    $result.message = 'Windows speech setup could not inspect or install the offline engine. Retry atlas-agent voice setup and check Windows component servicing.'
}
if ($result.fallback) { $result.message += ' The system language is unsupported by this offline engine; en-US was selected. This does not enable Turkish dictation.' }
$result | ConvertTo-Json -Compress -Depth 3
