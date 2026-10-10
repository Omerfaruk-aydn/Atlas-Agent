package remotecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Every script starts with this prelude. Paths are constants; arguments are
// constants or values this package generated (hashes, timestamps, tokens).
const prelude = `set -eu
if [ "$(id -u)" = 0 ]; then SUDO=""; else SUDO="sudo -n"; fi
D=` + RemoteDir + `
`

const lockScript = prelude + `
$SUDO mkdir -p "$D"
$SUDO chmod 0755 "$D"
if ! $SUDO mkdir "$D/.lock" 2>/dev/null; then
  owner="$($SUDO cat "$D/.lock/owner" 2>/dev/null || true)"
  tok="${owner%% *}"; ts="${owner##* }"
  case "$ts" in ''|*[!0-9]*) ts=0;; esac
  now="$(date +%s)"
  # Only this desktop's own lock or one abandoned for 30 minutes is taken over.
  if [ "$tok" != "$1" ] && [ $((now - ts)) -lt 1800 ]; then echo locked; exit 3; fi
fi
printf '%s %s\n' "$1" "$(date +%s)" | $SUDO tee "$D/.lock/owner" >/dev/null
[ -f "$D/.atlas-rc" ] || printf 'Managed by Atlas /rc\n' | $SUDO tee "$D/.atlas-rc" >/dev/null
echo ok
`

const unlockScript = prelude + `
owner="$($SUDO cat "$D/.lock/owner" 2>/dev/null || true)"
if [ "${owner%% *}" = "$1" ]; then $SUDO rm -rf "$D/.lock"; fi
echo ok
`

const packagesScript = prelude + `
$SUDO env DEBIAN_FRONTEND=noninteractive apt-get update -q
$SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends "$@"
$SUDO systemctl enable --now docker
echo ok
`

const firewallScript = prelude + `
for rule in "$@"; do $SUDO ufw allow "$rule" >/dev/null; done
echo ok
`

const backupScript = prelude + `
B="$D/backups/$1"
$SUDO mkdir -p "$B"
for f in compose.yaml Caddyfile .env state.json bin; do
  if [ -e "$D/$f" ]; then $SUDO cp -a "$D/$f" "$B/"; fi
done
$SUDO sh -c 'cd "$1" && ls -1 | sort | head -n -3 | while read -r old; do rm -rf -- "$old"; done' sh "$D/backups"
echo ok
`

// uploadScript writes stdin to a temporary file, verifies its SHA-256 and
// only then renames it into place, so a cut connection never leaves a
// partial file in use.
const uploadScript = prelude + `
dest="$1"; want="$2"; mode="$3"; owner="$4"
$SUDO mkdir -p "$(dirname "$dest")"
$SUDO sh -c 'umask 077; cat > "$1"' sh "$dest.new"
got="$($SUDO sha256sum "$dest.new" | cut -d' ' -f1)"
if [ "$got" != "$want" ]; then $SUDO rm -f "$dest.new"; echo mismatch; exit 4; fi
if [ -n "$owner" ]; then $SUDO chown "$owner" "$dest.new"; fi
$SUDO chmod "$mode" "$dest.new"
if $SUDO cmp -s "$dest.new" "$dest" 2>/dev/null; then $SUDO rm -f "$dest.new"; echo unchanged; exit 0; fi
$SUDO mv -f "$dest.new" "$dest"
echo changed
`

const composeArgs = `-f "$D/compose.yaml" --env-file "$D/.env" -p ` + ComposeName

const startScript = prelude + `
cd "$D"
$SUDO docker compose ` + composeArgs + ` up -d --remove-orphans
if [ "$#" -gt 0 ]; then $SUDO docker compose ` + composeArgs + ` restart "$@"; fi
echo ok
`

const psScript = prelude + `
$SUDO docker compose ` + composeArgs + ` ps --all --format '{{.Service}}:{{.State}}'
`

const logsScript = prelude + `
$SUDO docker compose ` + composeArgs + ` logs --no-color --tail 40 2>&1 | cut -c1-300
`

const rollbackScript = prelude + `
B="$D/backups/$1"
[ -d "$B" ] || { echo missing; exit 5; }
for f in compose.yaml Caddyfile .env state.json; do
  if [ -e "$B/$f" ]; then $SUDO cp -a "$B/$f" "$D/"; fi
done
if [ -e "$B/bin/atlas-mobile-relay" ]; then $SUDO cp -a "$B/bin/atlas-mobile-relay" "$D/bin/atlas-mobile-relay"; fi
cd "$D"
$SUDO docker compose ` + composeArgs + ` up -d --remove-orphans
$SUDO docker compose ` + composeArgs + ` restart relay
echo ok
`

// removeScript deletes only what /rc created: its Compose project and
// volumes, its directory (when it carries the /rc marker) and the key.
// Packages, firewall rules and other services are left alone.
const removeScript = prelude + `
if [ -f "$D/compose.yaml" ]; then
  cd "$D" && $SUDO docker compose ` + composeArgs + ` down -v --remove-orphans || true
  cd /
fi
if [ -f "$D/.atlas-rc" ]; then $SUDO rm -rf "$D"; fi
$SUDO rm -f ` + RemoteKeyFile + `
$SUDO rmdir ` + RemoteKeyDir + ` 2>/dev/null || true
echo ok
`

// stepTimeout bounds every remote step; package installation may be slow.
func stepTimeout(step string) time.Duration {
	switch step {
	case StepPackages:
		return 15 * time.Minute
	case StepStart, StepRollback, StepRemove:
		return 5 * time.Minute
	}
	return 2 * time.Minute
}

type remoteStep struct {
	r Runner
}

func (s remoteStep) run(ctx context.Context, step, script string, args []string, stdin []byte) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout(step))
	defer cancel()
	res, err := s.r.Run(ctx, script, args, stdin)
	if err != nil {
		return res, err
	}
	if res.Exit != 0 {
		return res, stepFailure(step, res)
	}
	return res, nil
}

func stepFailure(step string, res Result) *Error {
	out := strings.TrimSpace(string(res.Stdout))
	switch {
	case step == StepLock && strings.Contains(out, "locked"):
		return newError("locked", Msg("Another installation holds the server lock."), Msg("Wait for it to finish, then test again."))
	case strings.Contains(out, "mismatch"):
		return newError("checksum", Msg("The uploaded file failed its SHA-256 check; nothing was replaced."), Msg("Install again; the upload is repeated."))
	case step == StepRollback && strings.Contains(out, "missing"):
		return newError("no_backup", Msg("The backup to restore no longer exists on the server."), Message{})
	}
	return newError("step_failed", Msg("The step %q failed with exit code %d.", step, res.Exit), Msg("Open Diagnostics for the server output."))
}

// services parses `docker compose ps` output into service states.
func services(out []byte) map[string]string {
	m := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if svc, st, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
			m[svc] = st
		}
	}
	return m
}

// waitHealthy polls until relay and caddy have both been running for two
// consecutive checks, or the deadline passes.
func waitHealthy(ctx context.Context, s remoteStep, interval time.Duration, attempts int) (map[string]string, error) {
	var last map[string]string
	stable := 0
	for i := range attempts {
		if i > 0 {
			select {
			case <-ctx.Done():
				return last, ctx.Err()
			case <-time.After(interval):
			}
		}
		res, err := s.run(ctx, StepHealth, psScript, nil, nil)
		if err != nil {
			return last, err
		}
		last = services(res.Stdout)
		if last["relay"] == "running" && last["caddy"] == "running" {
			stable++
			if stable >= 2 {
				return last, nil
			}
			continue
		}
		stable = 0
	}
	return last, newError("unhealthy", Msg("The services did not stay running (relay: %s, caddy: %s).", orUnknown(last["relay"]), orUnknown(last["caddy"])), Msg("Open Diagnostics for the service logs."))
}

func stateJSON(plan Plan, keySHA string, now time.Time) []byte {
	data, _ := json.MarshalIndent(RemoteState{
		ManagedBy: "atlas-rc", Version: plan.Artifact.Version, SHA256: plan.Artifact.SHA256,
		Arch: plan.Artifact.Arch, PublicHost: plan.PublicHost,
		InstalledAt: now.UTC().Format(time.RFC3339), KeySHA256: keySHA,
	}, "", "  ")
	return append(data, '\n')
}

// upload writes content to a fixed remote path through uploadScript.
func (s remoteStep) upload(ctx context.Context, step, dest string, content []byte, mode, owner string) (bool, error) {
	res, err := s.run(ctx, step, uploadScript, []string{dest, sha256Hex(content), mode, owner}, content)
	if err != nil {
		return false, err
	}
	return bytes.Contains(res.Stdout, []byte("changed")) && !bytes.Contains(res.Stdout, []byte("unchanged")), nil
}
