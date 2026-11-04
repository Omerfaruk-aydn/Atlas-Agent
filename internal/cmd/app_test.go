package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestAppEventIsOneJSONLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	writeAppEvent(&buf, appEvent{Type: "ready", Port: 53122, Link: "http://127.0.0.1:53122/auth/launch?code=a&b=c"})

	line := buf.Bytes()
	require.Equal(t, byte('\n'), line[len(line)-1])
	require.Equal(t, 1, bytes.Count(line, []byte("\n")))

	var got map[string]any
	require.NoError(t, json.Unmarshal(line, &got))
	require.Equal(t, "ready", got["type"])
	require.EqualValues(t, 53122, got["port"])
	require.NotContains(t, got, "message")
}

func TestResolveAppProject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	cmd := &cobra.Command{}

	got, err := resolveAppProject(cmd, []string{dir})
	require.NoError(t, err)
	require.Equal(t, dir, got)

	_, err = resolveAppProject(cmd, []string{file})
	require.ErrorContains(t, err, "not a directory")

	_, err = resolveAppProject(cmd, []string{filepath.Join(dir, "missing")})
	require.Error(t, err)
}

func TestIsAtlasAppDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.False(t, isAtlasAppDir(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "something-else"}`), 0o644))
	require.False(t, isAtlasAppDir(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "atlas-app"}`), 0o644))
	require.True(t, isAtlasAppDir(dir))
}

func TestDesktopShellEnvDropsRunAsNode(t *testing.T) {
	t.Parallel()
	got := desktopShellEnv([]string{"PATH=x", "ELECTRON_RUN_AS_NODE=1", "HOME=y"})
	require.Equal(t, []string{"PATH=x", "HOME=y"}, got)
}

func TestDesktopShellArguments(t *testing.T) {
	t.Parallel()
	packaged := desktopShell{Electron: filepath.Join("bin", "app", packagedAppExe)}
	require.Equal(t, []string{"--project=p", "--atlas-bin=c"}, packaged.arguments("p", "c"))
	require.Equal(t, filepath.Join("bin", "app"), packaged.workDir())

	source := desktopShell{Electron: "electron", Dir: "app"}
	require.Equal(t, []string{"app", "--project=p", "--atlas-bin=c"}, source.arguments("p", "c"))
	require.Equal(t, "app", source.workDir())
}

func TestFindDesktopShellPrefersThePackagedApp(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, packagedAppExe)
	require.NoError(t, os.WriteFile(exe, nil, 0o755))
	t.Setenv("ATLAS_APP_DIR", dir)

	shell, err := findDesktopShell()
	require.NoError(t, err)
	require.Equal(t, desktopShell{Electron: exe}, shell)
}
