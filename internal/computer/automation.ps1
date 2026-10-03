$ErrorActionPreference = 'Stop'
try {
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
$p = [Console]::In.ReadToEnd() | ConvertFrom-Json
function Out-Result($value) { ConvertTo-Json -InputObject $value -Depth 8 -Compress | Write-Output }
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
            Out-Result @{text=$result.Text;lines=@($result.Lines | Select-Object -First 200 | ForEach-Object { @{text=$_.Text;words=@($_.Words | ForEach-Object { @{text=$_.Text;x=$_.BoundingRect.X;y=$_.BoundingRect.Y;width=$_.BoundingRect.Width;height=$_.BoundingRect.Height} })} })}
        } finally { $bitmap.Dispose() }
    } finally { $stream.Dispose() }
    exit
}
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$root = [System.Windows.Automation.AutomationElement]::RootElement
$children = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
function Finite-Number($value) { if ([double]::IsInfinity($value) -or [double]::IsNaN($value)) { return $null }; return $value }
function Describe($e) {
    $c = $e.Current
    @{element_id=($e.GetRuntimeId() -join ':');window_id=$(if($p.window_id){$p.window_id}else{[string]$c.NativeWindowHandle});native_handle=([string]$c.NativeWindowHandle);name=$(if($c.IsPassword){'[password]'}else{$c.Name});role=$c.ControlType.ProgrammaticName;automation_id=$c.AutomationId;process_id=$c.ProcessId;enabled=$c.IsEnabled;offscreen=$c.IsOffscreen;password=$c.IsPassword;x=(Finite-Number $c.BoundingRectangle.X);y=(Finite-Number $c.BoundingRectangle.Y);width=(Finite-Number $c.BoundingRectangle.Width);height=(Finite-Number $c.BoundingRectangle.Height)}
}
if ($p.action -eq 'windows') { Out-Result @($children | ForEach-Object { Describe $_ }); exit }
if ($p.window_id -notmatch '^\d+$') { throw 'Explicit numeric window_id required' }
$windows = @($children | Where-Object { [string]$_.Current.NativeWindowHandle -eq $p.window_id })
if ($windows.Count -ne 1) { throw 'Window disappeared; inspect windows again' }
$window = $windows[0]
if ($p.action -eq 'focus') { $window.SetFocus(); Out-Result @{focused=$true;window_id=$p.window_id}; exit }
# A breadth-first traversal caps both provider calls and returned elements.
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
$queue = [System.Collections.Generic.Queue[System.Windows.Automation.AutomationElement]]::new()
$queue.Enqueue($window)
$elements = [System.Collections.Generic.List[System.Windows.Automation.AutomationElement]]::new()
while ($queue.Count -gt 0 -and $elements.Count -lt 500) {
    $e = $queue.Dequeue()
    $elements.Add($e)
    $child = $walker.GetFirstChild($e)
    while ($null -ne $child -and $queue.Count -lt 500) { $queue.Enqueue($child); $child = $walker.GetNextSibling($child) }
}
if ($p.action -eq 'inspect') { Out-Result @{window_id=$p.window_id;truncated=($queue.Count -gt 0);elements=@($elements | ForEach-Object { Describe $_ })}; exit }
if (!$p.element_id -and !$p.name -and !$p.role) { throw 'Element identity, name or role required' }
$matches = @($elements | Where-Object { (!$p.element_id -or ($_.GetRuntimeId() -join ':') -eq $p.element_id) -and (!$p.name -or $_.Current.Name -eq $p.name) -and (!$p.role -or $_.Current.ControlType.ProgrammaticName -eq $p.role) })
if ($p.action -eq 'find') { Out-Result @{count=$matches.Count;matches=@($matches | ForEach-Object { Describe $_ })}; exit }
if ($p.action -eq 'assert' -and $p.condition -eq 'hidden') { Out-Result @{passed=($matches.Count -eq 0 -or @($matches | Where-Object { !$_.Current.IsOffscreen }).Count -eq 0)}; exit }
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
        'value' { if ($element.Current.IsPassword) { throw 'Password inspection prohibited' }; $pattern = $element.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern); $passed = $pattern.Current.Value -eq $p.expected }
        default { throw 'Unsupported assertion condition' }
    }
    Out-Result @{passed=$passed;window_id=$p.window_id}; exit
}
if (!$element.Current.IsEnabled -or $element.Current.IsOffscreen) { throw 'Target is disabled or offscreen' }
if ($p.action -eq 'invoke') { $pattern = $element.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern); $pattern.Invoke(); Out-Result @{action_sent=$true;verify_required=$true}; exit }
if ($p.action -eq 'set_value') { $pattern = $element.GetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern); if($pattern.Current.IsReadOnly){throw 'Target is read-only'}; $pattern.SetValue($p.text); Out-Result @{action_sent=$true;verify_required=$true}; exit }
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
    }
    Out-Result @{error_code=$code;recovery='Observe the current window and resolve the target again; use screenshot/OCR or manual handoff when the provider is unavailable.'}
}
