//go:build windows

package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNpmCommandWindowsRunsNodeWithoutShellQuoting(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for the npm update integration fixture")
	}
	dir := filepath.Join(t.TempDir(), "Ömer&Ceylin npm fixture")
	cli := filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")
	require.NoError(t, os.MkdirAll(filepath.Dir(cli), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "npm.cmd"), []byte("@echo off\r\nexit /b 99\r\n"), 0o644))
	require.NoError(t, os.WriteFile(cli, []byte("process.stdout.write(JSON.stringify(process.argv.slice(2)));"), 0o644))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	args := []string{"root", "-g", "value with spaces & Unicode: şğ"}
	command, err := npmCommand(t.Context(), args...)
	require.NoError(t, err)
	require.Equal(t, cli, command.Args[1])
	require.Equal(t, "node.exe", strings.ToLower(filepath.Base(command.Path)))
	output, err := command.Output()
	require.NoError(t, err)
	var received []string
	require.NoError(t, json.Unmarshal(output, &received))
	require.Equal(t, args, received)
	require.NoError(t, os.Remove(cli))
	_, err = npmCommand(t.Context(), "root", "-g")
	require.ErrorContains(t, err, "npm JavaScript entry point is unavailable")
}
