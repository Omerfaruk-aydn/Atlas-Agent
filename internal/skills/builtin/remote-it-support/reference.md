# Remote IT support reference

Command recipes for `remote_support` `exec`. Every argv is literal; start a
shell only when its syntax is needed. Check the remote OS from `status` first.
Read-only commands need the `command` grant; changes may also need `install`,
`services` or `restart`. Record the prior state before any change.

## Windows

### Inventory and resources (read-only)

- OS and build: `["cmd","/c","ver"]`, or
  `["powershell","-NoProfile","-Command","Get-CimInstance Win32_OperatingSystem | Select-Object Caption,Version,BuildNumber,LastBootUpTime | ConvertTo-Json"]`
- CPU and memory consumers:
  `["powershell","-NoProfile","-Command","Get-Process | Sort-Object CPU -Descending | Select-Object -First 15 Name,Id,CPU,WorkingSet64 | ConvertTo-Json"]`.
  Sample twice a few seconds apart before calling something a CPU hog.
- Disk capacity: `system_info` disks, or
  `["powershell","-NoProfile","-Command","Get-Volume | Select-Object DriveLetter,FileSystemLabel,Size,SizeRemaining,HealthStatus | ConvertTo-Json"]`.
- Disk health (separate from capacity):
  `["powershell","-NoProfile","-Command","Get-PhysicalDisk | Select-Object FriendlyName,MediaType,HealthStatus,OperationalStatus | ConvertTo-Json"]`
  and System log events from sources `disk`, `Ntfs`, `stornvme`.
- Large folders inside an allowed folder: `list`, then
  `["powershell","-NoProfile","-Command","Get-ChildItem -LiteralPath '<path>' -Directory | ForEach-Object { [pscustomobject]@{Name=$_.Name; Bytes=(Get-ChildItem -LiteralPath $_.FullName -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum} } | Sort-Object Bytes -Descending | ConvertTo-Json"]`.

### Application does not start

- Application and system errors from the last day:
  `["powershell","-NoProfile","-Command","Get-WinEvent -FilterHashtable @{LogName='Application'; Level=2; StartTime=(Get-Date).AddDays(-1)} -MaxEvents 30 | Select-Object TimeCreated,ProviderName,Id,Message | ConvertTo-Json"]`.
  Application Error (1000) and .NET Runtime (1026) events name the faulting
  module; Windows Error Reporting (1001) gives the bucket.
- Installed version: `["winget","list","--id","<Package.Id>","--exact"]` (read-only).
- Launch on the remote desktop with `launch` and confirm the window with
  `observe` / `automation`; do not infer from exit codes.

### Network and DNS

- `["ipconfig","/all"]`, `["route","print"]`, `["nslookup","<name>"]`,
  `["powershell","-NoProfile","-Command","Test-NetConnection <host> -Port 443 | ConvertTo-Json"]`.
- Distinguish: no link/IP (adapter), IP but no gateway (LAN), gateway but no
  DNS answer (resolver), DNS answer but no TCP (firewall/proxy/service).
- `ipconfig /flushdns` and adapter resets change state: record before, needs
  `services`.

### Services and startup

- State: `["sc","query","<name>"]` and `["sc","qc","<name>"]` (start type).
  Changing them needs `services`; record the previous start type.
- Startup apps: `["powershell","-NoProfile","-Command","Get-CimInstance Win32_StartupCommand | Select-Object Name,Command,Location | ConvertTo-Json"]`.

### Installation and removal

- Source: winget package ids from the publisher (`winget show --id <id>`
  shows publisher, version and installer hash) or the vendor's signed installer.
- Install (needs `command` and `install`):
  `["winget","install","--id","<id>","--exact","--silent","--accept-package-agreements","--accept-source-agreements"]`.
  An elevation prompt appears on the remote secure desktop: the person there
  must approve it. Atlas cannot and will not answer it.
- Verify afterwards: `winget list --id <id> --exact`, the program's
  `--version`, and opening it on the remote desktop when the complaint was
  "does not open".
- Rollback: `winget uninstall --id <id> --exact` (needs `install`), or the
  previous version with `--version`.
- Signature of a downloaded installer:
  `["powershell","-NoProfile","-Command","Get-AuthenticodeSignature '<file>' | Select-Object Status,SignerCertificate | ConvertTo-Json"]`
  and `Get-FileHash -Algorithm SHA256`.

## macOS

- Inventory: `["sw_vers"]`, `["uname","-a"]`, `["sysctl","-n","hw.memsize"]`.
- Processes: `["ps","-Ao","pid,pcpu,pmem,comm","-r"]` (sample twice).
- Disk capacity: `["df","-h"]`; health: `["diskutil","info","disk0"]`
  (SMART Status) — separate questions.
- App crashes: `["log","show","--last","1h","--predicate","eventType == logEvent AND messageType == error","--style","compact"]`
  and `~/Library/Logs/DiagnosticReports` through `list` when that folder is
  allowed.
- Network: `["scutil","--dns"]`, `["networksetup","-listallhardwareports"]`, `["nc","-vz","<host>","443"]`.
- Installs: Homebrew (`brew info`, `brew install`) or the vendor's notarized
  package; verify with `["spctl","-a","-vv","<app>"]` and the app version.

## Linux

- Inventory: `["cat","/etc/os-release"]`, `["uname","-a"]`, `["free","-m"]`, `["uptime"]`.
- Processes: `["ps","-eo","pid,pcpu,pmem,comm","--sort=-pcpu"]` (sample twice).
- Disk capacity: `["df","-h"]`; inode exhaustion: `["df","-i"]`; health:
  `["smartctl","-H","/dev/<disk>"]` when installed, and `journalctl -k` for I/O
  errors.
- Services: `["systemctl","status","<unit>","--no-pager"]`,
  `["journalctl","-u","<unit>","--since","-1h","--no-pager"]`; changes need
  `services`.
- Network: `["ip","addr"]`, `["ip","route"]`, `["resolvectl","status"]`,
  `["getent","hosts","<name>"]`.
- Installs: the distribution package manager (`apt-get`, `dnf`, `zypper`);
  verify with `dpkg -s` / `rpm -q` and the program's version.
