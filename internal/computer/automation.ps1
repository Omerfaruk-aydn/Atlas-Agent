$ErrorActionPreference = 'Stop'
try {
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
$p = [Console]::In.ReadToEnd() | ConvertFrom-Json
function Out-Result($value) { ConvertTo-Json -InputObject $value -Depth 8 -Compress | Write-Output }
if ($p.action -eq 'launch_app') {
    if (!$p.name) { throw 'Explicit installed application name required' }
    $apps = @(Get-StartApps | Where-Object { [string]::Equals($_.Name,[string]$p.name,[StringComparison]::OrdinalIgnoreCase) })
    if ($apps.Count -eq 0) { throw 'Target missing: installed application name was not found; inspect installed applications or ask for the exact name' }
    if ($apps.Count -ne 1) { throw 'Target ambiguous: installed application name matches multiple apps' }
    $appId = [string]$apps[0].AppID
    # ASCII identity validation must not depend on Turkish I/i case folding.
    if ($appId -cnotmatch '^[A-Za-z0-9_.!{}\\:-]+$') { throw 'Unsupported application identifier; use an explicit approved launcher' }
    Start-Process -FilePath (Join-Path $env:WINDIR 'explorer.exe') -ArgumentList @('shell:AppsFolder\' + $appId)
    Out-Result @{launch_requested=$true;application=$apps[0].Name;verify_required=$true}; exit
}
if ($p.action -eq 'monitors') {
    Add-Type -AssemblyName System.Windows.Forms
    Out-Result @( [System.Windows.Forms.Screen]::AllScreens | ForEach-Object {
        @{name=$_.DeviceName;primary=$_.Primary;x=$_.Bounds.X;y=$_.Bounds.Y;width=$_.Bounds.Width;height=$_.Bounds.Height}
    })
    exit
}
if ($p.action -eq 'ocr') {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $null = [Windows.Storage.StorageFile,Windows.Storage,ContentType=WindowsRuntime]
    $null = [Windows.Graphics.Imaging.BitmapDecoder,Windows.Graphics.Imaging,ContentType=WindowsRuntime]
    $null = [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime]
    function Await-Operation($operation, $type) {
        $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetGenericArguments().Count -eq 1 -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' } | Select-Object -First 1
        $task = $method.MakeGenericMethod($type).Invoke($null, @($operation))
        $task.Wait()
        return $task.Result
    }
    $file = Await-Operation ([Windows.Storage.StorageFile]::GetFileFromPathAsync($p.image_path)) ([Windows.Storage.StorageFile])
    $stream = Await-Operation ($file.OpenAsync(0)) ([Windows.Storage.Streams.IRandomAccessStream])
    try {
        $decoder = Await-Operation ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
        $bitmap = Await-Operation ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
        try {
            $engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
            if ($null -eq $engine) { throw 'No installed OCR language' }
            $result = Await-Operation ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
            function Normalize-OCRText([string]$value) {
                return (($value.Normalize().Replace([char]0x2019,[char]0x27).ToLowerInvariant() -replace "\s*'\s*", "'") -replace '\s+', ' ').Trim()
            }
            $matches = @()
            if ($p.name) {
                $target = Normalize-OCRText $p.name
                $targetWords = @($target -split ' ').Count
                foreach ($line in ($result.Lines | Select-Object -First 200)) {
                    $words = @($line.Words)
                    for ($i = 0; $i -lt $words.Count; $i++) {
                        for ($count = 1; $count -le [Math]::Min($targetWords+2,$words.Count-$i); $count++) {
                            $span = @($words[$i..($i+$count-1)])
                            $text = ($span | ForEach-Object { $_.Text }) -join ' '
                            if ((Normalize-OCRText $text) -ne $target) { continue }
                            $left = ($span | ForEach-Object { $_.BoundingRect.X } | Measure-Object -Minimum).Minimum
                            $top = ($span | ForEach-Object { $_.BoundingRect.Y } | Measure-Object -Minimum).Minimum
                            $right = ($span | ForEach-Object { $_.BoundingRect.X+$_.BoundingRect.Width } | Measure-Object -Maximum).Maximum
                            $bottom = ($span | ForEach-Object { $_.BoundingRect.Y+$_.BoundingRect.Height } | Measure-Object -Maximum).Maximum
                            $matches += @{text=$text;line_text=$line.Text;x=$left;y=$top;width=($right-$left);height=($bottom-$top);center_x=(($left+$right)/2);center_y=(($top+$bottom)/2)}
                        }
                    }
                }
            }
            Out-Result @{text=$result.Text;matches=$matches;match_count=$matches.Count;unique_match=($matches.Count -eq 1);lines=@($result.Lines | Select-Object -First 200 | ForEach-Object { @{text=$_.Text;words=@($_.Words | ForEach-Object { @{text=$_.Text;x=$_.BoundingRect.X;y=$_.BoundingRect.Y;width=$_.BoundingRect.Width;height=$_.BoundingRect.Height} })} })}
        } finally { $bitmap.Dispose() }
    } finally { $stream.Dispose() }
    exit
}
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
function Finite-Number($value) { if ([double]::IsInfinity($value) -or [double]::IsNaN($value)) { return $null }; return $value }
function Describe($e) {
    $c = $e.Current
    @{element_id=($e.GetRuntimeId() -join ':');window_id=$(if($p.window_id){$p.window_id}else{[string]$c.NativeWindowHandle});native_handle=([string]$c.NativeWindowHandle);name=$(if($c.IsPassword){'[password]'}else{$c.Name});role=$c.ControlType.ProgrammaticName;automation_id=$c.AutomationId;process_id=$c.ProcessId;enabled=$c.IsEnabled;offscreen=$c.IsOffscreen;password=$c.IsPassword;supported_patterns=@($e.GetSupportedPatterns() | ForEach-Object { $_.ProgrammaticName -replace 'PatternIdentifiers.Pattern$','' });x=(Finite-Number $c.BoundingRectangle.X);y=(Finite-Number $c.BoundingRectangle.Y);width=(Finite-Number $c.BoundingRectangle.Width);height=(Finite-Number $c.BoundingRectangle.Height)}
}
if ($p.action -eq 'windows') {
    $root = [System.Windows.Automation.AutomationElement]::RootElement
    $children = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
    Out-Result @($children | ForEach-Object { Describe $_ }); exit
}
if ($p.window_id -notmatch '^\d+$') { throw 'Explicit numeric window_id required' }
$window = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]::new([long]$p.window_id))
if ($null -eq $window) { throw 'Window disappeared; inspect windows again' }
# A breadth-first traversal caps both provider calls and returned elements.
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
$queue = [System.Collections.Generic.Queue[System.Windows.Automation.AutomationElement]]::new()
$queue.Enqueue($window)
$elements = [System.Collections.Generic.List[System.Windows.Automation.AutomationElement]]::new()
$limit = 500
if ($p.action -eq 'inspect') { $limit = 150 }
if ($p.max_elements -gt 0) { $limit = [int]$p.max_elements }
$clock = [System.Diagnostics.Stopwatch]::StartNew()
$truncated = $false
while ($queue.Count -gt 0 -and $elements.Count -lt $limit -and $clock.ElapsedMilliseconds -lt 2000) {
    $e = $queue.Dequeue()
    $elements.Add($e)
    # Runtime IDs are unique; exact identity avoids scanning unrelated controls.
    if ($p.element_id -and ($e.GetRuntimeId() -join ':') -eq $p.element_id) { $queue.Clear(); break }
    $child = $walker.GetFirstChild($e)
    while ($null -ne $child -and $queue.Count -lt $limit) { $queue.Enqueue($child); $child = $walker.GetNextSibling($child) }
    if ($null -ne $child) { $truncated = $true }
}
$truncated = $truncated -or $queue.Count -gt 0
if ($p.action -eq 'inspect') { Out-Result @{window_id=$p.window_id;truncated=$truncated;scanned_count=$elements.Count;elements=@($elements | ForEach-Object { Describe $_ })}; exit }
if (!$p.element_id -and !$p.name -and !$p.role) { throw 'Element identity, name or role required' }
$matches = @($elements | Where-Object { (!$p.element_id -or ($_.GetRuntimeId() -join ':') -eq $p.element_id) -and (!$p.name -or $_.Current.Name -eq $p.name) -and (!$p.role -or $_.Current.ControlType.ProgrammaticName -eq $p.role) })
if ($p.action -eq 'find') { Out-Result @{count=$matches.Count;truncated=$truncated;scanned_count=$elements.Count;matches=@($matches | ForEach-Object { Describe $_ })}; exit }
if ($truncated -and !($p.element_id -and $matches.Count -eq 1)) { throw 'Observation incomplete; resolve a unique runtime element_id with inspect/find or use a crop' }
if ($p.action -eq 'assert' -and $p.condition -eq 'hidden') { Out-Result @{passed=($matches.Count -eq 0 -or @($matches | Where-Object { !$_.Current.IsOffscreen }).Count -eq 0)}; exit }
if ($p.action -eq 'assert') {
    # Hidden duplicates cannot satisfy a visible control assertion.
    $matches = @($matches | Where-Object { !$_.Current.IsOffscreen })
} elseif ($p.action -eq 'invoke' -or $p.action -eq 'set_value') {
    $actionable = @($matches | Where-Object { $_.Current.IsEnabled -and !$_.Current.IsOffscreen })
    if ($matches.Count -gt 0 -and $actionable.Count -eq 0) { throw 'Target is disabled or offscreen' }
    $matches = $actionable
}
if ($p.action -eq 'assert' -and $matches.Count -eq 0) { Out-Result @{passed=$false;reason='target_missing'}; exit }
if ($matches.Count -eq 0) { throw 'Target missing' }
if ($matches.Count -gt 1) { throw 'Target ambiguous' }
$element = $matches[0]
if ($p.action -eq 'assert') {
    $passed = $false
    switch ($p.condition) {
        'visible' { $passed = !$element.Current.IsOffscreen }
        'enabled' { $passed = $element.Current.IsEnabled }
        'text' { $passed = !$element.Current.IsPassword -and $element.Current.Name -eq $p.expected }
        'value' { if ($element.Current.IsPassword) { throw 'Password inspection prohibited' }; $pattern = $null; if (!$element.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pattern)) { throw 'Pattern unavailable: Value; use observed visual input instead' }; $passed = $pattern.Current.Value -eq $p.expected }
        'keyboard_focused' { $passed = $element.Current.HasKeyboardFocus }
        default { throw 'Unsupported assertion condition' }
    }
    Out-Result @{passed=$passed;window_id=$p.window_id}; exit
}
if (!$element.Current.IsEnabled -or $element.Current.IsOffscreen) { throw 'Target is disabled or offscreen' }
if ($p.action -eq 'invoke') { $pattern = $null; if (!$element.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$pattern)) { throw 'Pattern unavailable: Invoke; use observed visual input instead' }; $pattern.Invoke(); Out-Result @{action_sent=$true;verify_required=$true}; exit }
if ($p.action -eq 'set_value') {
    if ($p.focus) {
        $element.SetFocus()
        if (!$element.Current.HasKeyboardFocus) { throw 'Field focus not confirmed; do not submit' }
    }
    $pattern = $null
    if (!$element.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pattern)) { throw 'Pattern unavailable: Value; use observed visual input instead' }
    if ($pattern.Current.IsReadOnly) { throw 'Target is read-only' }
    $pattern.SetValue($p.text)
    $verified = $null
    if (!$element.Current.IsPassword) {
        Start-Sleep -Milliseconds 50
        $verified = [string]::Equals($pattern.Current.Value, [string]$p.text, [StringComparison]::Ordinal)
        if (!$verified) { throw 'Value not applied; the provider accepted the call but readback differs. Observe the field and use focused keyboard input; do not submit or retry the same pattern blindly.' }
    }
    Out-Result @{action_sent=$true;verify_required=$true;value_verified=$verified;keyboard_focused=$element.Current.HasKeyboardFocus;element_id=($element.GetRuntimeId() -join ':');submission_required=$true}; exit
}
throw 'Unsupported automation action'
} catch {
    $code = 'accessibility_unavailable'
    switch -Wildcard ($_.Exception.Message) {
        'Explicit numeric*' { $code = 'invalid_target' }
        'Window disappeared*' { $code = 'target_missing' }
        'Target missing*' { $code = 'target_missing' }
        'Target ambiguous*' { $code = 'ambiguous_target' }
        'Target is disabled*' { $code = 'target_not_actionable' }
        'Target is read-only*' { $code = 'target_not_actionable' }
        'Password inspection*' { $code = 'password_protected' }
        'Unsupported*' { $code = 'unsupported_action' }
        'No installed OCR*' { $code = 'ocr_unavailable' }
        'Pattern unavailable*' { $code = 'unsupported_pattern' }
        'Observation incomplete*' { $code = 'observation_incomplete' }
        'Value not applied*' { $code = 'value_not_applied' }
        'Field focus not confirmed*' { $code = 'focus_denied' }
    }
    Out-Result @{error_code=$code;detail=$_.Exception.Message;recovery='Observe the current window and resolve the target again; use screenshot/OCR or manual handoff when the provider is unavailable.'}
}
