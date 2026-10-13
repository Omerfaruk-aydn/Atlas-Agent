		a, b string
		want float64
	}{
		{"#000000", "#FFFFFF", 21},
		{"#FFFFFF", "#FFFFFF", 1},
		{"#777777", "#FFFFFF", 4.48}, // The classic just-failing grey.
		{"#767676", "#FFFFFF", 4.54},
		{"#0000FF", "#FFFFFF", 8.59},
	} {
		a, err := parseHexColor(tc.a)
		require.NoError(t, err)
		b, err := parseHexColor(tc.b)
		require.NoError(t, err)
		require.InDelta(t, tc.want, contrastRatio(a, b), 0.01, tc.a)
	}
}

func mobileSystem() DesignSystem {
	return DesignSystem{
		Product:   "Atlas remote control",
		Platform:  "mobile",
		Direction: DesignDirection{Name: "Calm dark console", ChosenBy: "user", Alternatives: []string{"Bright minimal", "Playful"}},
		Colors:    map[string]string{"bg": "#0F1115", "text": "#E8EAED", "muted": "#5F6368", "accent": "#7AA2F7"},
		Pairs: []DesignColorPair{
			{Foreground: "text", Background: "bg", Usage: "text"},
			{Foreground: "muted", Background: "bg", Usage: "text"},
			{Foreground: "accent", Background: "bg", Usage: "ui"},
		},
		TypeScale: []int{12, 14, 17, 22, 28},
		SafeArea:  &DesignSafeArea{Top: 47, Bottom: 34},
		Viewports: []DesignViewport{{Name: "iPhone 15", Width: 393, Height: 852}},
		MinTouch:  44,
	}
}

func TestDesignSystemCheckMeasuresContrast(t *testing.T) {
	t.Parallel()
	report := checkDesignSystem(mobileSystem())
	require.False(t, report.Passed, "muted text on bg is too dark")
	byRule := map[string]DesignCheck{}
	for _, c := range report.Checks {
		byRule[c.Rule] = c
	}
	require.True(t, byRule["contrast text on bg (text)"].Passed)
	muted := byRule["contrast muted on bg (text)"]
	require.False(t, muted.Passed)
	require.Equal(t, "4.5:1", muted.Required)
	require.True(t, byRule["contrast accent on bg (ui)"].Passed)
	require.True(t, byRule["safe area declared"].Passed)
	require.True(t, byRule["phone portrait viewport"].Passed)
}

// Reported session: the user wanted mobile app screens and got web
// screens. A memory without the platform or the user's direction is
// refused, and phone designs need a safe area.
func TestDesignSystemRefusesGuessedPlatformAndDirection(t *testing.T) {
	t.Parallel()
	s := mobileSystem()
	s.Platform = ""
	require.ErrorContains(t, validateDesignSystem(s), "ask the user")
	s = mobileSystem()
	s.Direction = DesignDirection{}
	require.ErrorContains(t, validateDesignSystem(s), "let the user choose")
	s = mobileSystem()
	s.SafeArea = nil
	require.ErrorContains(t, validateDesignSystem(s), "safe_area is required")
	s = mobileSystem()
	s.Pairs = append(s.Pairs, DesignColorPair{Foreground: "nope", Background: "bg", Usage: "text"})
	require.ErrorContains(t, validateDesignSystem(s), "nope is not a color token")
	s = mobileSystem()
	s.Colors["bad"] = "blue"
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
