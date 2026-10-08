# Remote control with your own server (`/rc`)

## Try at home without a VPS

Run `/rc` and choose **Try on home network** (**Ev ağında dene** in Turkish).
Atlas detects active private IPv4 addresses automatically. If several exist,
choose the Ethernet or Wi-Fi adapter on the same network as your phone, rather
than a VPN or virtual-machine adapter. The computer may use Ethernet while the
phone uses Wi-Fi on the same router.

Atlas opens a temporary HTTPS listener on that address and an available port,
then shows the phone pairing QR. Scan it in Safari on iPhone or a browser on
Android, check the local certificate fingerprint, compare the six-digit code,
and accept with `a` in Atlas. A local certificate warning is expected. If
Windows asks, allow the development executable on your private home network.
Guest Wi-Fi isolation or a firewall can prevent access even on the same router.

The QR uses HTTPS. If a scanner or a manually entered address opens HTTP on
the same port, Atlas redirects GET/HEAD requests to HTTPS. The pairing fragment
stays in the browser; plaintext requests cannot pair a phone or control tasks.

Press `s` in the pairing view, or choose **Stop home network test** in `/rc`,
to disconnect. Closing the menu alone keeps the connection available; exiting
Atlas closes it. This mode does not write configuration, change the saved VPS
profile, connect to its relay, or alter its certificate and phone sessions.
Home pairings use separate local storage. Stopping the test does not silently
reconnect the VPS; explicitly turn remote access on, or restart Atlas to use its
saved startup configuration. Home testing must be started again explicitly
after restarting Atlas.

No phone application, account, VPS or identity verification is needed for this
test. It works on the same network only; access from outside still needs a
separately configured remote connection.

## VPS connection

`/rc` (or `/remote-control`) connects your own VPS to Atlas so you can use
Atlas from an iPhone (Safari) or Android phone (Chrome) anywhere. The phone
needs no app and no Tailscale; it opens an HTTPS page on your server.

```
phone ──HTTPS──▶ your VPS (Caddy + Atlas relay) ◀──outbound TLS── Atlas on your computer
```

Your computer never accepts connections from the internet. Atlas on the
computer opens an authenticated connection *out* to the relay, and phone
requests travel through it to the same phone panel `/mobile` serves locally.
Pairing, cookies, Host/Origin/CSRF checks and session lifetimes are the ones
described in [mobile-remote.md](mobile-remote.md); `/rc` only adds the server
management around them.

## What you need

- **A VPS you control** running **Ubuntu 22.04 or newer** or **Debian 12 or
  newer**, on **amd64 (x86_64) or arm64 (aarch64)**. One vCPU, 1 GB RAM and
  1 GiB free disk are enough for one Atlas installation and a few phones.
  Other systems are refused before anything is changed.
- **SSH access** as `root` or as a user with **passwordless sudo** (common
  default users such as `ubuntu` on most clouds qualify). Authenticate with a
  key file, your SSH agent or a password.
- **Docker with Compose v2.** On Ubuntu, `/rc` can install the distribution
  packages `docker.io` and `docker-compose-v2` as part of the plan you
  approve. On Debian, install Docker Engine with the Compose plugin yourself
  first (Docker's documentation); `/rc` tells you if it is missing.
- **A dedicated hostname**, such as `atlas.example.com`, whose DNS A and/or
  AAAA record points to the VPS. `/rc` checks DNS but never changes it.
- **Ports 80 and 443 reachable** on the VPS: TCP 80 and 443 (and UDP 443)
  must be free on the server and open in your provider's cloud firewall.
  `/rc` can add `ufw` rules; it does not change firewalld or cloud firewalls.
- **This computer awake with Atlas running.** When the computer sleeps or
  Atlas is closed, phones see "Atlas is offline". The relay cannot wake it.

The SSH address and the phone hostname are separate on purpose: you may SSH
to an IP address or an internal name while phones open the public HTTPS name.

## Step by step

1. Type `/rc`. The top of the dialog shows the real state: remote access
   (not set up, off, connecting, connected, disconnected or error), the
   server, the phone address, separate SSH, relay service, HTTPS and relay
   link checks, whether Atlas on this computer is connected, paired and
   recently active phones, pairings waiting for approval and the last check.
2. **Connect my existing server.** Fill in the profile name, server IP or SSH
   hostname, SSH port (22 by default), user, authentication method, key file
   (key method), password or key passphrase, the hostname phones will open
   and optionally an e-mail for certificate expiry notices. Errors appear
   under each field. Use Tab/Shift+Tab or ↑/↓ to move, ←/→ to change the
   authentication method, Ctrl+S to save and Esc to go back.
3. **Test connection.** On the first connection `/rc` shows the server's SSH
   host key fingerprint. Compare it with the one in your provider's web
   console, then press `t` to trust it. Nothing is sent before you decide.
   The test then runs read-only checks: system, architecture, privileges,
   Docker/Compose, disk and memory, ports 80/443, ufw/firewalld, an existing
   installation and its version, service health, DNS, HTTPS and the relay
   link. Anything that blocks installation is listed with what to do.
4. **Install on the server.** `/rc` shows the complete plan: components and
   versions, files and services, privileges, published ports, TLS method,
   firewall and package changes, conflicts and how to roll back. You approve
   it once in the normal permission dialog; the steps then run without
   further questions. If a later attempt would need anything new, you are
   asked again. Yolo mode does not skip this approval and Plan mode refuses
   it. Progress shows each real step; `c` cancels.
5. **Turn remote access on.** Atlas writes `mobile_relay_url` and
   `mobile_relay_token_file` to your global configuration and connects. The
   status becomes **Connected** only when the relay confirms the connection.
   Atlas reconnects automatically after network drops and when it starts.
6. **Pair a phone / show QR.** Scan the QR code with the phone camera, or
   open the address shown next to it. Compare the six-digit code on the phone
   with the one in Atlas and press `a` to accept. See below.

## What gets installed

All of it is the existing `deploy/mobile-relay` model:

- `/opt/atlas-mobile-relay/`: `compose.yaml`, `Caddyfile`, `.env`,
  `bin/atlas-mobile-relay`, `state.json`, `backups/` (three newest kept) and a
  `.atlas-rc` marker. `/rc` refuses to touch this directory if it exists
  without the marker.
- `/etc/atlas-mobile/connector.key`, mode 0600, readable only by the relay's
  unprivileged account (UID 65532).
- A Docker Compose project `atlas-mobile-relay` with two services: `caddy`
  (`caddy:2-alpine`) publishes 80/443 and obtains a Let's Encrypt or ZeroSSL
  certificate; `relay` runs the relay binary read-only in
  `gcr.io/distroless/static-debian12:nonroot` with no published port, no
  capabilities and no access logs.

The relay binary is **bundled inside the Atlas executable** for linux/amd64
and linux/arm64 (`go generate ./internal/remotecontrol/relaybin`, run by
`task build` and the release workflow). Its SHA-256 is recorded at build
time, checked again when Atlas reads it and checked on the server before the
file is put in place. Nothing is downloaded on the server except the two
container images above and, if approved, the distribution's Docker packages;
no third-party install scripts are run. The image tags are not pinned by
digest.

Installation is repeatable: unchanged files are not rewritten, the
connector key is kept, and services restart only when their binary,
key or configuration changed. A lock in `/opt/atlas-mobile-relay/.lock`
prevents two installations at once; a lock abandoned for 30 minutes is taken
over. Each step is recorded in Atlas' journal, so after a crash or a closed
Atlas, `/rc` shows the interrupted step and **Install** continues; finished
steps are checked and skipped. If an update does not start, the backup taken
by that run is restored automatically; **Restore the previous version** does
the same on demand.

"Ready" means more than exit codes: both containers must stay running,
`https://your-hostname` must present a valid certificate and answer as the
Atlas relay, and a request must travel through the relay to this computer.
If the certificate is still being issued or DNS is not ready, `/rc` says
which part is missing instead of claiming success.

## Pairing iPhone and Android

The QR code holds only `https://your-hostname/#pair=<nonce>`. The nonce is a
random single-use value valid for two minutes, kept in the URL fragment, so
it never reaches server logs and is not sent as a referrer. Showing a new QR
invalidates the previous one; an expired or reused code is refused and the
phone asks you to show a new one. The QR contains no SSH data, no connector
key and no session token.

1. Open the link in Safari (iPhone) or Chrome (Android).
2. The phone shows a six-digit code; `/mobile` (opened by **Pair a phone**)
   shows the same code next to the name the phone sent. That name is only a
   claim; the code is what identifies the phone.
3. Press `a` to accept or `r` to reject. Only then does the phone receive its
   session cookie.

If the terminal is too small for the QR, the dialog shows the address to open
instead. Adding the panel to the Home Screen is optional.

From the phone you can follow tasks, send prompts, answer questions and
permission requests, and pause, resume or stop work. Answers are checked
against the exact pending request; an answer to a request that was already
answered, canceled or expired is refused. After a network change or when the
phone returns from the background, the panel re-authenticates and reloads;
it retries with growing delays (up to 30 seconds) and never repeats an
action on its own.

## Devices and revoking access

**Manage devices** opens the device list: name, first pairing date, last
activity ("active now" when seen in the last two minutes). `d` unpairs one
phone, `Shift+X` (then `y`) unpairs all of them. Unpairing ends the phone's
session immediately, including an open live update stream.

These are separate actions with separate effects:

| Action | Effect |
| --- | --- |
| Unpair a phone | That phone loses access now. |
| Turn remote access off | This computer disconnects from the relay. The server keeps running. |
| Remove the service from the server | After approval, deletes the Compose project with its volumes, `/opt/atlas-mobile-relay` and the key. Packages and firewall rules stay. |
| Delete this profile | Erases the profile, its sealed secret and connector key on this computer only. The server is not contacted. |

**Stopping the server and its billing is done in your provider's panel.**
None of the actions above deletes the VPS or stops charges.

## Secrets

- Passwords and key passphrases are sealed with the operating system
  (DPAPI on Windows; `ATLAS_VAULT_KEY` elsewhere) under
  `<devices>/remote/secrets/`. The configuration stores only a reference
  (`secret_ref`). If sealing is unavailable, the secret is kept in memory
  until Atlas exits and `/rc` says so.
- Private keys are referenced by path (`ssh_key_file`) and never copied.
- The connector key is generated per profile under
  `<devices>/remote/profiles/<id>/connector.key` and copied to the server only
  over SSH, as standard input of a fixed command.
- None of these appear in the model's context, logs, telemetry, errors or the
  Diagnostics view, and the QR never contains them.
- Host keys you accept are stored in Atlas' own
  `<devices>/remote/known_hosts`. `~/.ssh/known_hosts` is read (unless the
  profile disables it) but never written. A changed host key stops the
  connection and shows both fingerprints; replacing it is an explicit
  `Shift+R`. A revoked key cannot be trusted.

User input never becomes shell syntax on the server: all remote commands are
fixed scripts, file contents travel on standard input, and the only
arguments are constants or values Atlas generated (hashes, timestamps).

## Help from the assistant

**Help me set up a server** asks the assistant to use the browser in your
provider's panel: find the server's IP, region, size, architecture and OS,
and check DNS. You sign in, pay and complete CAPTCHA or two-factor steps
yourself; purchases and paid changes need your explicit answer; page text is
treated as data. The assistant ends with an `atlas-remote-draft` block, and
**Use details found by the assistant** fills the form from it. The values are
untrusted: they are validated like typed input and nothing is saved until you
save. Installation itself always happens over SSH, never through the
browser. This works with any provider panel the browser tool can read; it is
not an automatic setup for every provider.

## Diagnostics and recovery

**Diagnostics and logs** shows the profile (secrets hidden), what the server
check found, every recorded step with its outcome and the relay's recent log
lines with known secrets removed.

- *Host key changed*: compare fingerprints with your provider before
  replacing. A reinstalled server legitimately changes it.
- *DNS*: create the A/AAAA record, wait for it to propagate, test again.
- *HTTPS not valid yet*: ports 80/443 must reach Caddy; certificates usually
  arrive within a minute.
- *Another installation in progress*: wait, or test again after 30 minutes.
- *Interrupted*: run **Install** again; it continues.
- Canceling stops Atlas from waiting; a command already running on the server
  (for example a package installation) may still finish there.

## Not verified live

This feature was developed and tested without a real VPS, DNS record or
physical phone. Verified: a real SSH server in-process (host key decisions,
authentication failures, timeouts, quoted commands), the deployment scripts'
lock/upload/backup behavior in a POSIX shell, the full install, update,
rollback, cancel/resume and removal sequences against a simulated server,
HTTPS and relay-link checks against the real relay over TLS, phone pairing
and revocation over the real gateway, and Android Chrome browser emulation.
Not verified: Docker, Caddy and Let's Encrypt on a real Ubuntu or Debian VPS,
real DNS, the iPhone WebKit browser tests in this environment, and a physical
iPhone or Android phone.
