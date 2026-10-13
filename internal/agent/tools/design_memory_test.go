	require.NoError(t, err)
	require.Equal(t, true, reset.(map[string]any)["reset"])
	got = designGet(t, dir)
	require.Equal(t, false, got["exists"])
	require.Equal(t, []int{1, 2}, got["history_versions"], "history survives a reset")
}

func TestDesignMemoryIsPerProjectAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	a, b := t.TempDir(), t.TempDir()
	sys := mobileSystem()
	_, err := runDesignSystem(t.Context(), a, DesignSystemParams{Action: "set", System: &sys}, time.Now)
	require.NoError(t, err)
	_, err = runDesignSystem(t.Context(), a, DesignSystemParams{Action: "approve"}, time.Now)
	require.NoError(t, err)

	require.Equal(t, false, designGet(t, b)["exists"], "another project never sees it")

	// A new tool instance (a restarted Atlas) reads the same memory.
	tool := NewDesignSystemTool(a)
	resp, err := tool.Run(t.Context(), fantasy.ToolCall{ID: "1", Name: DesignSystemToolName, Input: `{"action":"get"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `"status":"approved"`)
	require.Contains(t, resp.Content, "Calm dark console")
}

func TestDesignMemoryCorruptRecordIsExplicitAndKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(designSystemPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	const torn = `{"product": "half writ`
	require.NoError(t, os.WriteFile(path, []byte(torn), 0o644))

	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "get"}, time.Now)
	require.ErrorIs(t, err, errDesignCorrupt)
	require.ErrorContains(t, err, "ask the user")
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "package", Name: "x", Screens: []string{"a.png"}}, time.Now)
	require.Error(t, err, "nothing is delivered from a damaged memory")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, torn, string(data), "reads never touch the file")

	// A structurally valid file with an invalid system is damaged too.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":2,"approved":{"version":1,"system":{"product":"x"}}}`), 0o644))
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "get"}, time.Now)
	require.ErrorIs(t, err, errDesignCorrupt)

	sys := mobileSystem()
	now := func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	set, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &sys}, now)
	require.NoError(t, err)
	backup := set.(map[string]any)["corrupt_backup"].(string)
	require.Equal(t, ".atlas/design/system.json.corrupt-20261012T090000Z", backup)
	require.FileExists(t, filepath.Join(dir, filepath.FromSlash(backup)))
	require.Equal(t, "candidate", designGet(t, dir)["status"])
}

func TestDesignMemoryMigratesSchema1(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(designSystemPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	legacy := mobileSystem()
	legacy.UpdatedAt = "2026-10-01T10:00:00Z"
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	got := designGet(t, dir)
	require.Equal(t, "approved", got["status"])
	require.Equal(t, 1, got["migrated_from_schema"])
	require.Equal(t, 1, got["approved"].(*designRecord).Version)

	sys := mobileSystem()
	set, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &sys}, time.Now)
	require.NoError(t, err)
	require.Equal(t, 2, set.(map[string]any)["version"])
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"schema_version": 2`)
	require.Contains(t, string(data), "migrated from schema 1")

	// A file from a newer Atlas is neither read nor overwritten.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":9}`), 0o644))
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &sys}, time.Now)
	require.ErrorContains(t, err, "newer Atlas")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, `{"schema_version":9}`, string(data))
}

func TestDesignPackageRecordsApprovalAndIsNotDuplicated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sys := mobileSystem()
	sys.Colors["muted"] = "#9AA0A6"
	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &sys}, time.Now)
	require.NoError(t, err)
	writePNG(t, filepath.Join(dir, "shots", "home.png"), 393, 852)
	params := DesignSystemParams{Action: "package", Name: "candidate-v1", Screens: []string{"shots/home.png"}}
	res, err := runDesignSystem(t.Context(), dir, params, time.Now)
	require.NoError(t, err)
	require.False(t, res.(map[string]any)["quality_gate"].(designReport).Passed, "an unapproved system fails the gate")

	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "approve"}, time.Now)
	require.NoError(t, err)
	params.Name = "approved-v1"
	res, err = runDesignSystem(t.Context(), dir, params, time.Now)
	require.NoError(t, err)
	require.True(t, res.(map[string]any)["quality_gate"].(designReport).Passed)

	again, err := runDesignSystem(t.Context(), dir, params, time.Now)
	require.NoError(t, err)
	require.Equal(t, true, again.(map[string]any)["unchanged"], "the same delivery is not copied again")
	entries, err := os.ReadDir(filepath.Join(dir, "design-delivery"))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	writePNG(t, filepath.Join(dir, "shots", "home.png"), 393, 853)
	_, err = runDesignSystem(t.Context(), dir, params, time.Now)
	require.ErrorContains(t, err, "different content", "a changed delivery never overwrites the old one")
}
