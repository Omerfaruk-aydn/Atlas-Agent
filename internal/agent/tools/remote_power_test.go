	"sync/atomic"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func TestClassifyPowerCommand(t *testing.T) {
	t.Parallel()
	for argv, want := range map[string]string{
		"shutdown /s /t 0":                    powerShutdown,
		`C:\Windows\System32\shutdown.exe /p`: powerShutdown,
		"shutdown /r /t 0":                    powerRestart,
		"shutdown -h now":                     powerShutdown,
		"shutdown -r now":                     powerRestart,
		"poweroff":                            powerShutdown,
		"reboot":                              powerRestart,
		"systemctl poweroff":                  powerShutdown,
		"powershell -NoProfile -Command Stop-Computer -Force": powerShutdown,
		"pwsh -c Restart-Computer":                            powerRestart,
		"cmd /c shutdown /s /t 0":                             powerShutdown,
		"powershell -Command shutdown /r":                     powerRestart,
		"shutdown /a":                                         "",
		"shutdown /l":                                         "",
		"shutdown -c":                                         "",
		"notepad shutdown.txt":                                "",
		"git log --grep=shutdown":                             "",
		"powershell -Command Get-Process | Where-Object Name -eq x": "",
	} {
		got, ok := classifyPowerCommand(splitArgv(argv))
		require.Equal(t, want, got, argv)
		require.Equal(t, want != "", ok, argv)
	}
}

func splitArgv(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// Reported session: shutdown /s /t 0 was confirmed by the user and sent;
// the connection dropped as the computer turned off, and Atlas reported an
// unknown outcome and then a connection being restored. Now the drop is
// the expected result, later calls say why the computer is offline, and
// the request is never sent twice.
func TestRemoteShutdownDisconnectIsTheExpectedResult(t *testing.T) {
	t.Parallel()
	// The real host never gets the restart grant: if a request bypassed the
	// fake node it would be refused, never run.
	f := newSupportFixture(t, []support.Capability{support.CapCommand, support.CapInput})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	var execs, others atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/node/exec" {
			execs.Add(1)
			// The computer switches off mid-request.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		others.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(nodes.APIError{Code: "connection_lost", Message: "unavailable", Effect: "none"})
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()

	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	shutdown := map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "session_id": f.remote.SessionID, "argv": []string{"shutdown", "/s", "/t", "0"}}

	got := callSupport(t, tool, "shutdown-1", shutdown)
	require.True(t, got.resp.IsError, got.resp.Content)
	require.Equal(t, false, got.body["ok"])
	require.Equal(t, "unknown", got.body["effect"])
	require.Equal(t, "power_"+powerDisconnected, got.body["verification"])
	require.Contains(t, got.body["note"], "command outcome is still unknown")
	require.Equal(t, int32(1), execs.Load())

	// Asking again (a model retry, or "did it work?") sends nothing.
	again := callSupport(t, tool, "shutdown-2", shutdown)
	require.True(t, again.resp.IsError)
	require.Contains(t, again.resp.Content, "was not sent again")
	require.Equal(t, int32(1), execs.Load(), "never resent")

	// Status explains the offline computer instead of a connection fault.
	status := callSupport(t, tool, "status", map[string]any{"action": "status"})
	require.False(t, status.resp.IsError, status.resp.Content)
	view := status.body["result"].(map[string]any)
	require.Equal(t, "offline_after_shutdown", view["state"])

	// Other work is refused with the reason, nothing is sent.
	launch := callSupport(t, tool, "launch", map[string]any{"action": "launch", "device_id": f.remote.RemoteDeviceID, "argv": []string{"atlas-fixture-nonexistent-3b9d"}})
	require.True(t, launch.resp.IsError)
	require.Contains(t, launch.resp.Content, "asked to shutdown")
	require.Equal(t, int32(1), execs.Load())
}

func TestRemoteShutdownAcceptedBeforeTheDrop(t *testing.T) {
	t.Parallel()
	f := newSupportFixture(t, []support.Capability{support.CapCommand})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodes.Operation{ID: "op-x", State: "completed", Effect: "applied", ExitCode: new(int)})
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	got := callSupport(t, tool, "restart", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": []string{"shutdown", "/r", "/t", "5"}})
	require.False(t, got.resp.IsError, got.resp.Content)
	require.Equal(t, "power_"+powerAccepted, got.body["verification"])
	require.Contains(t, got.body["note"], "do not ask the user to confirm again")
}

func TestRemoteShutdownDeniedStaysAFailure(t *testing.T) {
	t.Parallel()
	// The owner's computer refusing the request is a real failure, not an
	// expected disconnect. A fake node answers, so no power command can
	// reach a real computer from this test.
	f := newSupportFixture(t, []support.Capability{support.CapCommand})
	_, client, err := f.ctl.Target()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(nodes.APIError{Code: string(support.CodeCapabilityDenied), Message: "needs restart", Effect: "none"})
	}))
	defer srv.Close()
	client.BaseURL, client.HTTP = srv.URL, srv.Client()
	tool := NewRemoteSupportTool(yolo(t), t.TempDir(), func() *support.Controller { return f.ctl })
	got := callSupport(t, tool, "denied", map[string]any{"action": "exec", "device_id": f.remote.RemoteDeviceID, "argv": []string{"shutdown", "/s", "/t", "0"}})
	require.True(t, got.resp.IsError)
	require.Equal(t, string(support.CodeCapabilityDenied), errCode(got))
	// Nothing was recorded, so a later approved attempt is not refused.
	_, recorded := (&powerLedger{}).get(f.remote.RemoteDeviceID, f.remote.SessionID)
	require.False(t, recorded)
}
