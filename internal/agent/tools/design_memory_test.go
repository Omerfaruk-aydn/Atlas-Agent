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
