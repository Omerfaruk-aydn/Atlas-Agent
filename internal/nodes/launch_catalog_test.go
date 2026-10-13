	catalog *computer.AppCatalog
}

func (c *catalogDesktop) Apps() *computer.AppCatalog { return c.catalog }

func (c *catalogDesktop) LaunchApp(context.Context, computer.LaunchPlan) (int, error) {
	panic("node launches go through procown")
}

func (c *catalogDesktop) VisibleWindows(context.Context) ([]computer.AppWindowObservation, error) {
	return nil, nil
}

func newCatalogServer(t *testing.T) (*Server, string) {
	t.Helper()
	chrome := filepath.Join(t.TempDir(), "Atlas Fixture Chrome", "chrome-fixture-7c1e.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(chrome), 0o755))
	require.NoError(t, os.WriteFile(chrome, []byte("MZ"), 0o644))
	entries := []computer.AppEntry{{
		Name:         "Google Chrome",
		Aliases:      []string{"Chrome", "Krom"},
		Launch:       computer.AppLauncher{Method: computer.LaunchExecutable, Path: chrome},
		Capabilities: []string{"launch", "browser_profiles"},
		Profiles:     []computer.BrowserProfile{{Directory: "Profile 3", Name: "Ömer Faruk"}, {Directory: "Profile 7", Name: "Ömer"}, {Directory: "Profile 8", Name: "Ömer"}},
	}}
	desk := &catalogDesktop{
		fakeDesktop: &fakeDesktop{size: computer.Size{Width: 800, Height: 600}},
		catalog:     computer.NewAppCatalog(func(context.Context) ([]computer.AppEntry, error) { return entries, nil }, time.Minute),
	}
	return &Server{opts: ServerOptions{Desktop: desk}}, chrome
}

// Reported session: launch argv ["chrome"] failed because chrome.exe is
// not on PATH. The node now resolves it like the Run dialog does.
func TestNodeLaunchResolvesAProgramNotOnPath(t *testing.T) {
	t.Parallel()
	s, chrome := newCatalogServer(t)
	argv, plan, apiErr := s.resolveLaunch(t.Context(), LaunchRequest{Argv: []string{"chrome", "https://www.google.com"}})
	require.Nil(t, apiErr)
	require.Equal(t, []string{chrome, "https://www.google.com"}, argv)
	require.NotNil(t, plan)
}

func TestNodeLaunchByAppWithProfile(t *testing.T) {
	t.Parallel()
	s, chrome := newCatalogServer(t)
	argv, plan, apiErr := s.resolveLaunch(t.Context(), LaunchRequest{App: "krom", Args: []string{"https://www.google.com"}, Profile: "ömer faruk aydın"})
	require.Nil(t, apiErr)
	require.Equal(t, []string{chrome, "--profile-directory=Profile 3", "https://www.google.com"}, argv)
	require.Equal(t, "Profile 3", plan.Profile.Directory)
}

func TestNodeLaunchRefusesAnAmbiguousProfile(t *testing.T) {
	t.Parallel()
	s, _ := newCatalogServer(t)
	_, _, apiErr := s.resolveLaunch(t.Context(), LaunchRequest{App: "chrome", Profile: "Ömer"})
	require.NotNil(t, apiErr)
	require.Equal(t, "profile_ambiguous", apiErr.Code)
	require.Equal(t, "none", apiErr.Effect)
}

func TestNodeLaunchRequestShapes(t *testing.T) {
	t.Parallel()
	s, _ := newCatalogServer(t)
	for name, req := range map[string]LaunchRequest{
		"empty":            {},
		"both":             {Argv: []string{"x"}, App: "chrome"},
		"profile on argv":  {Argv: []string{"x"}, Profile: "Ömer Faruk"},
		"unknown app":      {App: "Atlas nonexistent 9f2c"},
		"raw profile flag": {App: "chrome", Args: []string{"--profile-directory=Default"}},
	} {
		_, _, apiErr := s.resolveLaunch(t.Context(), req)
		require.NotNil(t, apiErr, name)
	}
	// A path is literal and never rewritten.
	argv, plan, apiErr := s.resolveLaunch(t.Context(), LaunchRequest{Argv: []string{`C:\Tools\x.exe`}})
	require.Nil(t, apiErr)
	require.Nil(t, plan)
	require.Equal(t, []string{`C:\Tools\x.exe`}, argv)
}
