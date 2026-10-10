package remotecontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Fixed locations on the server. They are constants, never user input.
const (
	RemoteDir     = "/opt/atlas-mobile-relay"
	RemoteKeyDir  = "/etc/atlas-mobile"
	RemoteKeyFile = RemoteKeyDir + "/connector.key"
	ComposeName   = "atlas-mobile-relay"
	// relayUID is the distroless nonroot account that reads the key.
	relayUID = "65532"
)

// composeFile mirrors deploy/mobile-relay/compose.yaml but runs the
// bundled, checksum-verified binary instead of building from source on the
// server. Only Caddy publishes ports.
const composeFile = `# Managed by Atlas /rc. Manual edits are replaced on the next install.
name: ` + ComposeName + `
services:
  relay:
    image: gcr.io/distroless/static-debian12:nonroot
    entrypoint: ["/atlas-mobile-relay"]
    command:
      - serve
      - --listen=0.0.0.0:8787
      - --behind-proxy
      - --public-url=https://${ATLAS_MOBILE_HOST:?Set the dedicated HTTPS hostname}
      - --token-file=/run/secrets/connector_key
    volumes:
      - ./bin/atlas-mobile-relay:/atlas-mobile-relay:ro
    secrets:
      - connector_key
    user: "` + relayUID + `:` + relayUID + `"
    restart: unless-stopped
    read_only: true
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    # No published ports: this unencrypted listener belongs only to Caddy.
    networks:
      - mobile
  caddy:
    image: caddy:2-alpine
    environment:
      ATLAS_MOBILE_HOST: ${ATLAS_MOBILE_HOST:?Set the dedicated HTTPS hostname}
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    restart: unless-stopped
    networks:
      - mobile
secrets:
  connector_key:
    file: ` + RemoteKeyFile + `
networks:
  mobile:
volumes:
  caddy_data:
  caddy_config:
`

// caddyFile is deploy/mobile-relay/Caddyfile with an optional ACME contact.
// Access logs stay off: request paths and headers belong to the phone.
func caddyFile(email string) string {
	var b strings.Builder
	b.WriteString("# Managed by Atlas /rc. Manual edits are replaced on the next install.\n")
	if email != "" {
		b.WriteString("{\n    email " + email + "\n}\n\n")
	}
	b.WriteString(`{$ATLAS_MOBILE_HOST} {
    reverse_proxy relay:8787 {
        flush_interval -1
    }
    header {
        Strict-Transport-Security "max-age=31536000"
        X-Content-Type-Options "nosniff"
        Referrer-Policy "no-referrer"
        X-Frame-Options "DENY"
    }
}
`)
	return b.String()
}

func envFile(host string) string {
	return "# Managed by Atlas /rc.\nATLAS_MOBILE_HOST=" + host + "\n"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// preflightScript only reads. It prints key=value lines; values are bounded
// and parsed defensively. $SUDO is used for reads that need root, and only
// when sudo works without a password.
const preflightScript = `
set +e
kv() { printf '%s=%s\n' "$1" "$(printf '%s' "$2" | tr -d '\r\n' | cut -c1-300)"; }
kv uname_s "$(uname -s 2>/dev/null)"
kv uname_m "$(uname -m 2>/dev/null)"
if [ -r /etc/os-release ]; then
  kv os_id "$(. /etc/os-release; printf '%s' "${ID:-}")"
  kv os_like "$(. /etc/os-release; printf '%s' "${ID_LIKE:-}")"
  kv os_version "$(. /etc/os-release; printf '%s' "${VERSION_ID:-}")"
  kv os_name "$(. /etc/os-release; printf '%s' "${PRETTY_NAME:-}")"
fi
kv uid "$(id -u 2>/dev/null)"
SUDO=""
if [ "$(id -u 2>/dev/null)" = 0 ]; then kv sudo root
elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then kv sudo passwordless; SUDO="sudo -n"
else kv sudo none; fi
kv systemd "$(command -v systemctl >/dev/null 2>&1 && echo yes || echo no)"
kv apt "$(command -v apt-get >/dev/null 2>&1 && echo yes || echo no)"
kv disk_kb "$(df -Pk / 2>/dev/null | awk 'NR==2{print $4}')"
kv mem_kb "$(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null)"
kv addresses "$(hostname -I 2>/dev/null)"
if command -v docker >/dev/null 2>&1; then
  kv docker yes
  kv docker_version "$($SUDO docker version --format '{{.Server.Version}}' 2>/dev/null)"
  kv compose_version "$($SUDO docker compose version --short 2>/dev/null)"
else
  kv docker no
fi
if command -v ss >/dev/null 2>&1; then
  kv tcp_listen "$(ss -Hltn 2>/dev/null | awk '{print $4}' | tr '\n' ' ')"
  kv udp_listen "$(ss -Hlun 2>/dev/null | awk '{print $4}' | tr '\n' ' ')"
else
  kv tcp_listen unknown
fi
if [ -n "$SUDO" ] || [ "$(id -u 2>/dev/null)" = 0 ]; then
  if command -v ufw >/dev/null 2>&1; then
    st="$($SUDO ufw status 2>/dev/null)"
    case "$st" in *"Status: active"*) kv ufw active;; *) kv ufw inactive;; esac
    kv ufw_rules "$(printf '%s\n' "$st" | awk '/ALLOW/{print $1}' | tr '\n' ' ')"
  else
    kv ufw none
  fi
  kv firewalld "$(systemctl is-active firewalld 2>/dev/null)"
  if [ -d ` + RemoteDir + ` ]; then
    kv installed yes
    kv managed "$([ -f ` + RemoteDir + `/.atlas-rc ] && echo yes || echo no)"
    kv state_b64 "$($SUDO cat ` + RemoteDir + `/state.json 2>/dev/null | head -c 4000 | base64 | tr -d '\n')"
    kv lock "$($SUDO cat ` + RemoteDir + `/.lock/owner 2>/dev/null | head -c 200)"
    kv binary_sha "$($SUDO sha256sum ` + RemoteDir + `/bin/atlas-mobile-relay 2>/dev/null | cut -d' ' -f1)"
    kv compose_sha "$($SUDO sha256sum ` + RemoteDir + `/compose.yaml 2>/dev/null | cut -d' ' -f1)"
    kv backups "$($SUDO ls -1 ` + RemoteDir + `/backups 2>/dev/null | tr '\n' ' ')"
    if [ "$(command -v docker)" ]; then
      kv services "$($SUDO docker compose -f ` + RemoteDir + `/compose.yaml --env-file ` + RemoteDir + `/.env ps --all --format '{{.Service}}:{{.State}}' 2>/dev/null | tr '\n' ' ')"
    fi
  else
    kv installed no
  fi
  kv key_exists "$($SUDO test -f ` + RemoteKeyFile + ` && echo yes || echo no)"
  kv key_sha "$($SUDO sha256sum ` + RemoteKeyFile + ` 2>/dev/null | cut -d' ' -f1)"
  kv project_containers "$($SUDO docker ps --filter label=com.docker.compose.project=` + ComposeName + ` --format '{{.Names}}' 2>/dev/null | tr '\n' ' ')"
fi
kv done yes
`
