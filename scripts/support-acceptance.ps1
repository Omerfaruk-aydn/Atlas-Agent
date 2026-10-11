# Live acceptance of /support between two real Windows computers.
#
# Computer A (receives support) runs:
#   atlas support host --allow system_info,file_read,file_send,file_receive,command,screen_view,input --folder Work=$env:TEMP\atlas-support-acceptance
# and answers the connection request with y after comparing the
# verification code.
#
# Computer B (supports) runs this script with the code A printed:
#   powershell -ExecutionPolicy Bypass -File scripts\support-acceptance.ps1 -Atlas .\dist\dev\atlas-it-support-dev.exe -Code ABCD-EFGH-JKMN@192.168.1.20:51515
#
# When the script says so, end the session on A with Ctrl+C. The script then
# checks that B's later operation is refused and that B's own files and
# desktop were not used.
param(
    [Parameter(Mandatory = $true)][string]$Atlas,
    [Parameter(Mandatory = $true)][string]$Code
)
$ErrorActionPreference = 'Stop'

$work = Join-Path $env:TEMP ("atlas-support-b-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Path $work | Out-Null
Set-Location $work
$marker = 'atlas-acceptance-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
$notepadBefore = @(Get-Process -Name notepad -ErrorAction SilentlyContinue).Count
$filesBefore = @(Get-ChildItem -Path $work -Recurse -File).Count

Write-Host "B working folder: $work"
Write-Host "Step 1-6: system info, create a file on A, pull it, open Notepad on A, observe A's screen."
$out = & $Atlas support join $Code --sysinfo `
    --exec "powershell -NoProfile -Command Set-Content -Path accept.txt -Value $marker -NoNewline" `
    --pull "Work/accept.txt:from-A.txt" `
    --launch "notepad.exe" `
    --observe 2>&1 | Out-String
Write-Host $out

$results = @()
$buffer = ''
foreach ($line in ($out -split "`r?`n")) {
    if ($line -match '^\{') { $buffer = $line } elseif ($buffer) { $buffer += "`n" + $line }
    if ($buffer -and $line -match '^\}') { $results += ($buffer | ConvertFrom-Json); $buffer = '' }
}
function Step($action) { $results | Where-Object { $_.action -eq $action } | Select-Object -First 1 }
$fail = @()

$sys = Step 'system_info'
if (-not $sys.ok) { $fail += 'system_info failed' } else { Write-Host ("A: {0} {1}, device {2}" -f $sys.result.hostname, $sys.result.os_version, $sys.remote_device_id) }
$exec = Step 'exec'
if (-not $exec.ok -or $exec.result.state -ne 'completed') { $fail += 'creating the file on A failed' }
$pull = Step 'pull'
if (-not $pull.ok) { $fail += 'pull failed' } else {
    $local = Join-Path $work 'from-A.txt'
    $hash = (Get-FileHash -Algorithm SHA256 $local).Hash.ToLower()
    if ($hash -ne $pull.result.sha256) { $fail += "hash mismatch: $hash vs $($pull.result.sha256)" }
    if ((Get-Content -Raw $local) -ne $marker) { $fail += 'pulled content differs' }
    Write-Host "Pulled file verified: sha256 $hash"
}
$launch = Step 'launch'
if (-not $launch.ok) { $fail += 'launching Notepad on A failed (needs an unlocked desktop on A)' }
$observe = Step 'observe'
if (-not $observe.ok) { $fail += 'observing A failed' } elseif (($observe.result.windows | ConvertTo-Json -Depth 6) -notmatch 'Notepad|Not Defteri|notepad') {
    $fail += 'Notepad window not seen on A'
} else { Write-Host 'Notepad window observed on A.' }
foreach ($r in $results) {
    if ($r.remote_device_id -ne $sys.remote_device_id) { $fail += "step $($r.step) names another device" }
}

Write-Host ''
Write-Host 'Step 7-8: connect again and keep the session open; now end it on A with Ctrl+C.'
Write-Host 'Codes are single use: run atlas support host again on A and enter its new code here.'
$next = Read-Host 'New code from A'
$wait = & $Atlas support join $next --wait 2>&1 | Out-String
Write-Host $wait
if ($wait -notmatch 'Session ended: (revoked|disconnected)') { $fail += 'the end of the session was not reported' }

Write-Host 'Step 9: a later operation from B must be refused.'
$again = & $Atlas support join $next --sysinfo 2>&1 | Out-String
if ($again -notmatch 'pairing_used|pairing_expired|remote_offline|pairing_invalid|session_revoked') { $fail += 'a later operation was not refused' }

$notepadAfter = @(Get-Process -Name notepad -ErrorAction SilentlyContinue).Count
if ($notepadAfter -ne $notepadBefore) { $fail += "Notepad count on B changed ($notepadBefore -> $notepadAfter)" }
$filesAfter = @(Get-ChildItem -Path $work -Recurse -File | Where-Object { $_.Name -ne 'from-A.txt' }).Count
if ($filesAfter -ne $filesBefore) { $fail += 'unexpected files appeared on B' }
if (Test-Path (Join-Path $work 'accept.txt')) { $fail += 'the remote command wrote on B' }

if ($fail.Count -gt 0) {
    Write-Host 'FAILED:' -ForegroundColor Red
    $fail | ForEach-Object { Write-Host " - $_" -ForegroundColor Red }
    exit 1
}
Write-Host 'PASSED: pairing, acceptance, system info, file creation, verified transfer, remote Notepad, owner disconnect, refusal and B unchanged.' -ForegroundColor Green
