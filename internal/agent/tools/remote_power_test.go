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
