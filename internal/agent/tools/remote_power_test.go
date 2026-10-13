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
