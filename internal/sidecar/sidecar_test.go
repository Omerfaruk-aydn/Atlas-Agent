package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary stand in for a sidecar process and for the
// agent executable.
func TestMain(m *testing.M) {
	switch os.Getenv("SIDECAR_TEST_MODE") {
	case "sleep":
		time.Sleep(time.Hour)
		os.Exit(0)
	case "exit1":
		fmt.Println("boom")
		os.Exit(1)
	case "agent":
		prompt, _ := io.ReadAll(os.Stdin)
		session := os.Getenv("SIDECAR_TEST_SESSION")
		for i, a := range os.Args {
			if a == "--session" && i+1 < len(os.Args) {
				session = os.Args[i+1]
			}
		}
		out, _ := json.Marshal(Conversation{SessionID: session, Response: "echo: " + string(prompt), Cost: 0.5})
		fmt.Println(string(out))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// installHelper copies the test binary into a sidecar folder so a sidecar can
// name it with a relative command.
func installHelper(t *testing.T, project, id string) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	src, err := os.Open(exe)
	require.NoError(t, err)
	defer src.Close()
	dir := filepath.Join(Dir(project), id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	dst, err := os.OpenFile(filepath.Join(dir, "helper.exe"), os.O_CREATE|os.O_WRONLY, 0o755)
	require.NoError(t, err)
	defer dst.Close()
	_, err = io.Copy(dst, src)
	require.NoError(t, err)
	return "./helper.exe"
}

func writeSidecar(t *testing.T, project, id string, def Definition) {
	t.Helper()
	dir := filepath.Join(Dir(project), id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.Marshal(def)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sidecar.json"), data, 0o644))
}

func TestDiscoverValidatesAndStartsDisabled(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	writeSidecar(t, project, "board", Definition{Command: "node", Args: []string{"main.mjs"}})
	writeSidecar(t, project, "abs", Definition{Command: "/usr/bin/evil"})
	writeSidecar(t, project, "traversal", Definition{Command: "../x"})
	writeSidecar(t, project, "badpolicy", Definition{Command: "x", RestartPolicy: "sometimes"})
	require.NoError(t, os.MkdirAll(filepath.Join(Dir(project), "Bad_Name"), 0o755))

	found, errs := Discover(Dir(project))
	require.Len(t, found, 1)
	require.Equal(t, "board", found[0].ID)
	require.False(t, found[0].Enabled, "a sidecar never starts until the user enables it")
	require.Equal(t, RestartAlways, found[0].Def.Policy())
	require.Len(t, errs, 4)

	before := found[0].Version
	require.NoError(t, SetEnabled(Dir(project), "board", true))
	found, _ = Discover(Dir(project))
	require.True(t, found[0].Enabled)
	require.NotEqual(t, before, found[0].Version, "enabling changes the version")
	require.Error(t, SetEnabled(Dir(project), "ghost", true))
	require.Error(t, SetEnabled(Dir(project), "../board", true))
}

func newManager(project string) *Manager {
	return &Manager{
		Project: project, Exe: "atlas", Poll: 50 * time.Millisecond,
		BackoffBase: 20 * time.Millisecond, BackoffMax: 40 * time.Millisecond, StableAfter: time.Hour,
	}
}

func statusOf(m *Manager, id string) Info {
	for _, i := range m.Status() {
		if i.ID == id {
			return i
		}
	}
	return Info{}
}

func TestManagerRunsEnabledSidecarsAndStopsThem(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	writeSidecar(t, project, "idle", Definition{Command: installHelper(t, project, "idle"), Env: map[string]string{"SIDECAR_TEST_MODE": "sleep"}})
	writeSidecar(t, project, "off", Definition{Command: installHelper(t, project, "off"), Env: map[string]string{"SIDECAR_TEST_MODE": "sleep"}})
	require.NoError(t, SetEnabled(Dir(project), "idle", true))

	m := newManager(project)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = m.Run(ctx) }()

	require.Eventually(t, func() bool { return statusOf(m, "idle").Status == StatusRunning }, 5*time.Second, 25*time.Millisecond)
	require.Equal(t, Info{}, statusOf(m, "off"), "a disabled sidecar is not started")
	require.DirExists(t, filepath.Join(RuntimeDir(project, "idle"), "logs"))

	// The user disables it: the manager stops the process.
	require.NoError(t, SetEnabled(Dir(project), "idle", false))
	require.Eventually(t, func() bool { return statusOf(m, "idle").Status == StatusStopped }, 5*time.Second, 25*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("manager did not stop")
	}
}

func TestManagerRestartPolicies(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	crash := map[string]string{"SIDECAR_TEST_MODE": "exit1"}
	writeSidecar(t, project, "always", Definition{Command: installHelper(t, project, "always"), Env: crash})
	writeSidecar(t, project, "never", Definition{Command: installHelper(t, project, "never"), Env: crash, RestartPolicy: RestartNever})
	for _, id := range []string{"always", "never"} {
		require.NoError(t, SetEnabled(Dir(project), id, true))
	}
	m := newManager(project)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = m.Run(ctx) }()

	require.Eventually(t, func() bool { return statusOf(m, "always").Restarts >= 2 }, 5*time.Second, 25*time.Millisecond, "an always-restart sidecar keeps coming back with backoff")
	require.Eventually(t, func() bool { return statusOf(m, "never").Status == StatusErrored }, 5*time.Second, 25*time.Millisecond)
	require.Equal(t, 0, statusOf(m, "never").Restarts)
	require.NotEmpty(t, statusOf(m, "never").LastErr)
}

func TestEnvCarriesTheDataAndAgentapiLocations(t *testing.T) {
	t.Parallel()
	m := newManager("/proj")
	env := m.Env(Sidecar{ID: "board", Def: Definition{Env: map[string]string{"X": "1"}}})
	joined := fmt.Sprint(env)
	require.Contains(t, joined, "ATLAS_SIDECAR_ID=board")
	require.Contains(t, joined, "ATLAS_AGENTAPI_EXE=atlas")
	require.Contains(t, joined, "ATLAS_PROJECT_DIR=/proj")
	require.Contains(t, joined, "X=1")
	require.Contains(t, joined, "ATLAS_SIDECAR_TOKEN=")
}

func TestAgentAPIRecordsEventsAndRefusesAutoPermissions(t *testing.T) {
	t.Setenv("SIDECAR_TEST_MODE", "agent")
	exe, err := os.Executable()
	require.NoError(t, err)
	api := AgentAPI{Binary: exe, Project: t.TempDir(), EventsDir: filepath.Join(t.TempDir(), "events")}

	t.Setenv("SIDECAR_TEST_SESSION", "sess-1")
	conv, err := api.NewConversation(t.Context(), "hello")
	require.NoError(t, err)
	require.Equal(t, "sess-1", conv.SessionID)
	require.Equal(t, "echo: hello", conv.Response)

	conv, err = api.SendMessage(t.Context(), "sess-1", "again")
	require.NoError(t, err)
	require.Equal(t, "echo: again", conv.Response)

	meta, err := api.GetMetadata("sess-1")
	require.NoError(t, err)
	require.Equal(t, 2, meta.Messages)
	require.InDelta(t, 1.0, meta.Cost, 0.001)
	_, err = api.GetMetadata("other")
	require.Error(t, err)

	args, err := api.args("s")
	require.NoError(t, err)
	require.Contains(t, args, "readonly")
	require.Contains(t, args, "--session")

	api.Permissions = "auto"
	_, err = api.NewConversation(t.Context(), "x")
	require.ErrorContains(t, err, "not allowed for a sidecar")
	api.Permissions = "edits"
	_, err = api.SendMessage(t.Context(), "", "x")
	require.Error(t, err)
	_, err = api.NewConversation(t.Context(), "  ")
	require.Error(t, err)
}
