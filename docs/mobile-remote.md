# Phone access from anywhere, without a phone application

> **Easiest path:** type `/rc` in Atlas. It connects your VPS over SSH,
> shows an installation plan for approval, installs and verifies the relay
> and pairs your phone. See [remote-control.md](remote-control.md). The
> manual steps below remain valid.

Atlas can serve the same phone panel in iPhone Safari and Android Chrome.
Neither phone needs Tailscale, an APK or an App Store application. Home Screen
installation is optional; opening the HTTPS page in the browser is enough.

The desktop opens an outbound TLS WebSocket to **your own relay**. Phone
requests travel over individual outbound connections to the existing mobile
HTTP handler. The desktop does not accept inbound internet connections, need
administrator privileges for this mode, or require home router forwarding.

You still need a reachable server and a dedicated HTTPS hostname. This code
does not provision a VPS, purchase a domain or create an internet endpoint
by itself. No public deployment has been performed as part of this change.

## Size and scope

For one Atlas installation and a few phones, start with **1 vCPU, 1 GB RAM
and 10 GB SSD**. This is a conservative starting estimate, not a measured
capacity guarantee. The server runs only the standalone relay and HTTPS
proxy. Atlas, its tools and any local models stay on the desktop; cloud model
requests still go to the selected provider.

The development standalone builds are about 8 MiB (Windows/Linux). An idle
Windows smoke run used about 7.8 MiB working set; that excludes Caddy, the OS,
active transfers and build tools, and is not a Linux load-test result.

File uploads and optional screenshot previews determine bandwidth. The
relay permits at most 32 simultaneous phone requests, including event
streams, and caps each request body at 9 MiB. The existing phone attachment
rules impose their own smaller format/size limits. Build binaries elsewhere
if the VPS has limited RAM or disk; compiling the complete repository and
retaining build caches is not part of this runtime size estimate.

One relay instance belongs to **one Atlas installation and one dedicated
origin**. For several desktops, use separate relay instances and hostnames.
Do not host an unrelated application on the same origin: phone cookies and
the service worker belong to that origin.

## 1. Create the connector key

Use either the full Atlas CLI or the standalone relay executable:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\atlas-mobile-secrets"
atlas-agent mobile-relay keygen --out "$env:LOCALAPPDATA\atlas-mobile-secrets\connector.key"
```

The command creates 32 random bytes encoded as base64url. Existing files are
never overwritten, and the key is not printed. Transfer this file privately
to your server. Keep it outside the repository, deployment directory, web
root and backups shared with other people. This is a machine connector key,
not a phone login or an API key for a model provider.

On Windows, restrict the file's ACL to the account running Atlas. On Linux,
use mode `0600` and the relay service account. Rotating this shared key
requires updating both ends and restarting the relay and `/mobile`.

## 2. Deploy the standalone server

Build the small executable without the agent runtime:

```powershell
$env:CGO_ENABLED = "0"
$env:GOEXPERIMENT = "greenteagc"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o atlas-mobile-relay ./cmd/atlas-mobile-relay
```

For an ARM64 server such as Oracle Ampere A1, use `GOARCH=arm64` instead.

Run this on the target server behind your HTTPS reverse proxy:

```sh
./atlas-mobile-relay serve \
  --listen 127.0.0.1:8787 \
  --public-url https://atlas.example.com \
  --token-file /etc/atlas-mobile/connector.key
```

Forward `atlas.example.com` to `127.0.0.1:8787`, preserving the Host header
and supporting WebSocket upgrades and unbuffered SSE responses. A public
HTTPS certificate must cover this hostname. If terminating TLS directly,
use `--tls-cert` and `--tls-key` together instead of a proxy.

An unencrypted listener is restricted to a loopback IP by default.
`--behind-proxy` is an explicit opt-in for a **private** container/proxy
network; its listener must never be published on the public host. HTTPS is
always required in the configured public URL and outbound connector.

### Docker Compose with Caddy

Files are in `deploy/mobile-relay/`. Copy `.env.example` to `.env` there and
set your real dedicated hostname and absolute key path. The DNS A/AAAA
records must point to the VPS, and public ports 80/443 must reach Caddy.
Only Caddy publishes ports; the relay uses the private Compose network.

The relay container runs as UID/GID 65532. For the bind-backed Compose
secret, make the source key readable by that account, without broadening it
to other users:

```sh
sudo chown 65532:65532 /etc/atlas-mobile/connector.key
sudo chmod 600 /etc/atlas-mobile/connector.key
docker compose --env-file .env up -d --build
```

Run Compose from `deploy/mobile-relay/`. The Docker build context uses a
Dockerfile-specific allowlist of the standalone service source: `.atlas`,
provider configurations, OAuth credentials and local artifacts are not sent
to the build. Caddy stores certificate state in persistent Docker volumes.
The template intentionally does not enable HTTP access logs containing
phone request paths or private headers. Docker/Caddy deployment has not been
live-tested on a public VPS in this workspace.

## 3. Connect the desktop

Add these settings to the desktop's existing Atlas configuration, preserving
its other fields. Replace the file path and hostname with your real values:

```json
{
  "tools": {
    "devices": {
      "mobile_lan": false,
      "mobile_listen": "127.0.0.1:7777",
      "mobile_relay_url": "https://atlas.example.com",
      "mobile_relay_token_file": "C:/Users/YourName/AppData/Local/atlas-mobile-secrets/connector.key"
    }
  }
}
```

Start the new Atlas build and open `/mobile`. The initial authenticated TLS
handshake must succeed before a public pairing link is shown. The dialog
then displays the public address and connected/reconnecting status. With
relay settings absent, the existing local HTTPS mode remains available.
Invalid or incomplete relay settings do not silently select another server.

The desktop's generated certificate still identifies the local gateway. In
relay mode, the browser trusts the public relay's HTTPS certificate; the
local certificate fingerprint shown inside the panel is not the public
server's TLS fingerprint.

## 4. Use either phone

1. Open the current short-lived pairing link in Safari or Chrome. The nonce
   is in its fragment, not a server query string or a permanent credential.
2. Compare the phone's six-digit code with `/mobile` and accept on the
   desktop. A trusted person operating that desktop can perform this step;
   there is no unauthenticated remote approval endpoint. Pair once before
   leaving home, or coordinate the initial approval through a trusted channel.
3. Use the panel to read tasks/messages, send prompts and attachments,
   answer questions/permissions and pause/resume/stop supported workflows.
4. Keep the desktop awake, connected and Atlas running. A sleeping or shut
   down computer is offline; the relay cannot start it remotely.

On either phone, switching Wi-Fi/mobile networks or returning from the
background refreshes authentication and snapshots. Unsent prompt text and
answers to an unchanged question remain drafts. Resolved/replaced questions
do not inherit old answers. Safari Home Screen installation remains optional.
There is no background push service; open the panel to see pending requests.

## Failure and trust boundaries

- A missing desktop returns 503; a mid-request disconnect returns 502.
  Neither response means a mutation definitely had no effect. Refresh and
  inspect the current state before deliberately retrying an uncertain action.
- Each HTTP request uses a fresh tunnel connection. Reconnecting the control
  channel never replays an HTTP request, including a POST with an idempotency
  header. Existing phone action idempotency remains enforced by Atlas.
- Host, Origin, CSRF, cookie, pairing, session expiry, device revocation,
  permission identity and workflow revision checks use the original gateway.
  Forwarded headers from a phone cannot select another desktop or destination.
- The connector cannot act as a general TCP/HTTP proxy. It invokes only the
  configured in-process phone handler, with bounded frames and requests.
- Only one connector can attach. A duplicate is refused; stream invitations
  are random, short-lived and single-use. A browser Origin is refused on
  connector WebSocket routes, even if a key has been disclosed.
- TLS protects both internet legs. **The relay is trusted with plaintext
  phone traffic after TLS termination**, including cookies, prompts and
  responses. This is not end-to-end encryption against the relay operator.
  Host it on infrastructure you control; the connector key alone is not a
  phone authorization, but a compromised relay remains a privileged endpoint.
- Changing the relay origin requires a new phone pairing. Connector keys
  never appear in URLs, browser storage, QR codes or ordinary error output.

## Verification

Go integration tests use real TLS WebSockets and HTTP, covering large-body
transfer, concurrent SSE, wrong Host/Origin/CSRF, rejected connectors,
duplicate attachment, unknown streams, uncertain mutation failure,
reconnection without replay, offline responses and original mobile pairing,
idempotency, task control and logout. TLS verification is never disabled in
the relay tests; the fixtures trust an explicit test certificate authority.

Five WebKit/iPhone and four Chromium/Android browser tests cover responsive
layouts, drafts, foreground/network return and authentication expiry. These
are emulation tests. A physical phone over a real public deployment has not
been tested; no usable public address exists until your VPS/DNS are configured.
