	require.NotContains(t, v, "command")

	// A tool that supplies no content gets no invented diff.
	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{SessionID: "s1", ToolName: "write", Params: map[string]any{"file_path": "a.txt"}}, v)
	require.Equal(t, "a.txt", v["file_path"])
	require.NotContains(t, v, "diff")

	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{SessionID: "s1", ToolName: "bash", Params: map[string]any{"command": "go test ./... " + strings.Repeat("y", 5000)}}, v)
	require.Contains(t, v["command"], "go test")
	require.LessOrEqual(t, len(v["command"].(string)), maxCommandBytes+len("…"))
	require.NotContains(t, v, "diff")
	require.NotContains(t, v, "file_path")

	// Params that are not an object are ignored rather than failing.
	v = map[string]any{}
	permissionDetails(permission.PermissionRequest{Params: "plain"}, v)
	require.Empty(t, v)
}
