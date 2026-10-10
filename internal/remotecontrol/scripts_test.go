package remotecontrol

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"
)

var allScripts = map[string]string{
	"preflight": preflightScript, "lock": lockScript, "unlock": unlockScript, "packages": packagesScript,
	"firewall": firewallScript, "backup": backupScript, "upload": uploadScript, "start": startScript,
	"ps": psScript, "logs": logsScript, "rollback": rollbackScript, "remove": removeScript,
}

// The scripts are checked with a POSIX shell's parser when one is present
// (Git for Windows, any Unix). They are never executed here.
func TestRemoteScriptsParse(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell to parse the scripts")
	}
	for name, script := range allScripts {
		out, err := exec.CommandContext(t.Context(), sh, "-n", "-c", script).CombinedOutput()
		require.NoError(t, err, "%s: %s", name, out)
	}
}

func TestStartScriptRestartsOnlyRequestedServices(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell")
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "unchanged"},
		{name: "relay", args: []string{"relay"}},
		{name: "caddy", args: []string{"caddy"}},
		{name: "both", args: []string{"relay", "caddy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Replace only the external executable with a shell function.
			// The production script itself decides which commands to run.
			script := strings.Replace(startScript, "cd \"$D\"", `
docker() { printf 'docker %s\n' "$*"; }
cd "$D"`, 1)
			out, code := localScript(t, sh, filepath.ToSlash(t.TempDir()), script, "", tc.args...)
			require.Zero(t, code, out)
			lines := strings.Split(out, "\n")
			require.True(t, strings.HasSuffix(lines[0], " up -d --remove-orphans"), out)
			if len(tc.args) == 0 {
				require.Equal(t, []string{lines[0], "ok"}, lines, "unchanged install must not restart services")
			} else {
				require.Len(t, lines, 3, out)
				require.True(t, strings.HasSuffix(lines[1], " restart "+strings.Join(tc.args, " ")), out)
				require.Equal(t, "ok", lines[2])
			}
		})
	}
	t.Run("restart failure", func(t *testing.T) {
		t.Parallel()
		script := strings.Replace(startScript, "cd \"$D\"", `
docker() {
  for arg in "$@"; do if [ "$arg" = restart ]; then return 7; fi; done
}
cd "$D"`, 1)
		out, code := localScript(t, sh, filepath.ToSlash(t.TempDir()), script, "", "relay")
		require.Equal(t, 7, code, out)
		require.NotContains(t, out, "ok")
	})
}

// localScript points a script at dir instead of the server paths and runs
// it without sudo, so its real shell behavior can be exercised.
func localScript(t *testing.T, sh, dir, script string, stdin string, args ...string) (string, int) {
	t.Helper()
	script = strings.Replace(script, `if [ "$(id -u)" = 0 ]; then SUDO=""; else SUDO="sudo -n"; fi`, `SUDO=""`, 1)
	script = strings.ReplaceAll(script, "D="+RemoteDir, "D="+shellQuote(dir))
	cmd := exec.CommandContext(t.Context(), sh, append([]string{"-c", script, "sh"}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return strings.TrimSpace(string(out)), code
}

func TestLockUploadAndBackupScriptsBehaveInARealShell(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell")
	}
	for _, tool := range []string{"sha256sum", "cmp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("missing " + tool)
		}
	}
	dir := filepath.ToSlash(t.TempDir())

	out, code := localScript(t, sh, dir, lockScript, "", "tok-a")
	require.Equal(t, 0, code, out)
	out, code = localScript(t, sh, dir, lockScript, "", "tok-b")
	require.Equal(t, 3, code, "a fresh lock of another desktop is respected: %s", out)
	_, code = localScript(t, sh, dir, lockScript, "", "tok-a")
	require.Equal(t, 0, code, "the owner may retake its own lock")
	localScript(t, sh, dir, unlockScript, "", "tok-b")
	_, err = os.Stat(filepath.Join(dir, ".lock"))
	require.NoError(t, err, "another desktop cannot release the lock")
	localScript(t, sh, dir, unlockScript, "", "tok-a")
	_, err = os.Stat(filepath.Join(dir, ".lock"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(dir, ".atlas-rc"))
	require.NoError(t, err, "the directory is marked as created by /rc")

	dest := dir + "/compose.yaml"
	out, code = localScript(t, sh, dir, uploadScript, "v1", dest, sha256Hex([]byte("tampered")), "0644", "")
	require.Equal(t, 4, code, out)
	_, err = os.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist, "a failed checksum replaces nothing")
	out, _ = localScript(t, sh, dir, uploadScript, "v1", dest, sha256Hex([]byte("v1")), "0644", "")
	require.Equal(t, "changed", out)
	out, _ = localScript(t, sh, dir, uploadScript, "v1", dest, sha256Hex([]byte("v1")), "0644", "")
	require.Equal(t, "unchanged", out)
	data, _ := os.ReadFile(dest)
	require.Equal(t, "v1", string(data))

	for _, stamp := range []string{"20260101T000000Z", "20260102T000000Z", "20260103T000000Z", "20260104T000000Z"} {
		out, code = localScript(t, sh, dir, backupScript, "", stamp)
		require.Equal(t, 0, code, out)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	require.Equal(t, []string{"20260102T000000Z", "20260103T000000Z", "20260104T000000Z"}, names, "only the three newest backups are kept")
	backup, _ := os.ReadFile(filepath.Join(dir, "backups", "20260104T000000Z", "compose.yaml"))
	require.Equal(t, "v1", string(backup))
}

func TestRemoteScriptsTakeNoUserInputAsShellSyntax(t *testing.T) {
	t.Parallel()
	for name, script := range allScripts {
		// User values reach scripts only as positional parameters ("$1") or
		// as standard input; nothing is spliced into the script text.
		require.NotContains(t, script, "eval ", name)
		require.NotContains(t, script, "curl", name, "no download happens on the server")
		require.NotContains(t, script, "wget", name)
	}
	require.NotContains(t, removeScript, "ufw", "removal leaves firewall rules alone")
	require.Contains(t, removeScript, `if [ -f "$D/.atlas-rc" ]`, "only a directory /rc created is deleted")
}

func TestGeneratedComposeKeepsRelayPrivateAndReadOnly(t *testing.T) {
	t.Parallel()
	var compose struct {
		Name     string
		Services map[string]struct {
			Image    string
			Ports    []string
			Volumes  []string
			Secrets  []string
			ReadOnly bool `yaml:"read_only"`
			User     string
			CapDrop  []string `yaml:"cap_drop"`
		}
		Secrets map[string]struct{ File string }
	}
	require.NoError(t, yaml.Unmarshal([]byte(composeFile), &compose))
	require.Equal(t, ComposeName, compose.Name)
	relay := compose.Services["relay"]
	require.Empty(t, relay.Ports)
	require.True(t, relay.ReadOnly)
	require.Equal(t, "65532:65532", relay.User)
	require.Equal(t, []string{"ALL"}, relay.CapDrop)
	require.Equal(t, []string{"./bin/atlas-mobile-relay:/atlas-mobile-relay:ro"}, relay.Volumes)
	require.Equal(t, RemoteKeyFile, compose.Secrets["connector_key"].File)
	require.Equal(t, []string{"80:80", "443:443", "443:443/udp"}, compose.Services["caddy"].Ports)

	require.NotContains(t, caddyFile(""), "email")
	require.Contains(t, caddyFile("ops@example.com"), "email ops@example.com")
	require.NotContains(t, strings.ToLower(caddyFile("")), "log", "access logs stay off")
}
