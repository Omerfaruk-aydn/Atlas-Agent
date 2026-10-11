# /support live acceptance

Automated tests run both endpoints on one computer with separate state
directories, identities and working folders. That proves the protocol and the
permission boundaries, but it is not a real two-computer, LAN or NAT test.
This procedure is the real one.

## Requirements

- Two Windows computers, A (receives support) and B (supports), with the same
  Atlas build (for example `dist\dev\atlas-it-support-dev.exe`).
- Same local network for the LAN run. For the internet run, a relay you
  operate (`atlas support relay`) reachable over HTTPS from both, and the same
  relay URL and token file configured on both.
- A unlocked and signed in (for Notepad and screen observation).

## LAN run

On A:

```powershell
atlas support host --allow system_info,file_read,file_send,file_receive,command,screen_view,input --folder Work=$env:TEMP\atlas-support-acceptance
```

On B, with the `CODE@address` A printed:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\support-acceptance.ps1 -Atlas .\dist\dev\atlas-it-support-dev.exe -Code ABCD-EFGH-JKMN@192.168.1.20:51515
```

Compare the verification code on both screens and accept on A with `y`. The
script reads A's system information, creates a file in A's folder, pulls it
to B and verifies its SHA-256, opens Notepad on A and observes it, then asks
for a second code, waits while A ends the session with Ctrl+C, checks that a
later operation is refused, and checks that B's Notepad count and B's files
did not change.

## Internet run

Same steps with `--internet --relay https://relay.example --relay-token-file <file>`
on A and the bare code (no address) plus the same relay flags on B.

## Recording the result

Record the date, both OS versions, network type (LAN, relay, NAT), the
script's PASSED/FAILED output and any step that needed manual action (UAC on
A, firewall prompts). A failure of the Notepad step with
`remote_desktop_unavailable` means A's desktop was locked or not interactive.
