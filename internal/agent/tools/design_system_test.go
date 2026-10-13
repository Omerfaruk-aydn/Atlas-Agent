	require.ErrorContains(t, validateDesignSystem(s), "#RRGGBB")
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))))
}

func TestDesignSystemMemoryAndPackage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) }
	got, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "get"}, now)
	require.NoError(t, err)
	require.Equal(t, false, got.(map[string]any)["exists"])

	sys := mobileSystem()
	sys.Colors["muted"] = "#9AA0A6"
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &sys}, now)
	require.NoError(t, err)
	got, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "get"}, now)
	require.NoError(t, err)
	stored := got.(map[string]any)["system"].(DesignSystem)
	require.Equal(t, "mobile", stored.Platform)
	require.Equal(t, "Calm dark console", stored.Direction.Name)

	writePNG(t, filepath.Join(dir, "shots", "Home Screen.png"), 393, 852)
	writePNG(t, filepath.Join(dir, "shots", "settings.png"), 1440, 900) // A web frame slipped in.
	res, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "package", Name: "remote-mobile-v1", Screens: []string{"shots/Home Screen.png", "shots/settings.png"}, OpenQuestions: []string{"Dark mode only"}}, now)
	require.NoError(t, err)
	gate := res.(map[string]any)["quality_gate"].(designReport)
	require.False(t, gate.Passed, "the landscape frame fails the phone gate")

	pkg := filepath.Join(dir, "design-delivery", "remote-mobile-v1")
	data, err := os.ReadFile(filepath.Join(pkg, "manifest.json"))
	require.NoError(t, err)
	var m designManifest
	require.NoError(t, json.Unmarshal(data, &m))
	require.Len(t, m.Screens, 2)
	require.Equal(t, "screens/01-home-screen.png", m.Screens[0].File)
	require.Len(t, m.Screens[0].SHA256, 64)
	require.FileExists(t, filepath.Join(pkg, "screens", "01-home-screen.png"))
	readme, err := os.ReadFile(filepath.Join(pkg, "README.md"))
	require.NoError(t, err)
	require.Contains(t, string(readme), "Quality gate: FAILED")
	require.Contains(t, string(readme), "Dark mode only")

	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "package", Name: "remote-mobile-v1", Screens: []string{"shots/Home Screen.png"}}, now)
	require.ErrorContains(t, err, "already exists", "packages are never overwritten")
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "package", Name: "x", Screens: []string{"../outside.png"}}, now)
	require.ErrorContains(t, err, "inside the working directory")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shots", "broken.png"), []byte("\x89PNG\r\n\x1a\nnot really"), 0o644))
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "package", Name: "y", Screens: []string{"shots/broken.png"}}, now)
	require.Error(t, err)
	require.NoDirExists(t, filepath.Join(dir, "design-delivery", "y"))
}
