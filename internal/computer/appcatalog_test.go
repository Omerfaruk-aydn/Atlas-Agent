	require.Equal(t, entries[1].Launch.Path, e.Launch.Path)
}

func TestCatalogNoticesARemovedApplication(t *testing.T) {
	t.Parallel()
	files := map[string]bool{userChrome: true}
	c := fixtureCatalog(t, files)
	_, err := c.Resolve(t.Context(), "chrome")
	require.NoError(t, err)
	gen := c.Generation()

	delete(files, userChrome) // Uninstalled inside the cache window.
	_, err = c.Resolve(t.Context(), "exe:chrome")
	require.ErrorIs(t, err, ErrAppNotFound, "rediscovery no longer lists it")
	require.Greater(t, c.Generation(), gen)
}

func TestCatalogInvalidateOnTargetChange(t *testing.T) {
	t.Parallel()
	calls := 0
	c := NewAppCatalog(func(context.Context) ([]AppEntry, error) {
		calls++
		return nil, nil
	}, time.Hour)
	_, _ = c.List(t.Context(), "", false)
	_, _ = c.List(t.Context(), "", false)
	require.Equal(t, 1, calls, "cached")
	c.Invalidate()
	_, _ = c.List(t.Context(), "", false)
	require.Equal(t, 2, calls)
	require.Equal(t, uint64(2), c.Generation())
}

func TestMatchProfile(t *testing.T) {
	t.Parallel()
	profiles := []BrowserProfile{{"Default", "Kişisel"}, {"Profile 3", "Ömer Faruk"}, {"Profile 4", "İş"}}
	// Reported session: the user typed the full name "ömer faruk aydın".
	p, err := MatchProfile(profiles, "ömer faruk aydın")
	require.NoError(t, err)
	require.Equal(t, "Profile 3", p.Directory)
	p, err = MatchProfile(profiles, "Profile 4")
	require.NoError(t, err)
	require.Equal(t, "İş", p.Name)
	p, err = MatchProfile(profiles, "is")
	require.NoError(t, err)
	require.Equal(t, "Profile 4", p.Directory)

	_, err = MatchProfile(profiles, "Ayşe")
	require.ErrorIs(t, err, ErrProfileNotFound)

	twins := []BrowserProfile{{"Profile 1", "Ömer"}, {"Profile 2", "Ömer Faruk"}, {"Profile 5", "Ömer Faruk"}}
	_, err = MatchProfile(twins, "ömer faruk")
	require.ErrorIs(t, err, ErrProfileAmbig)
	require.ErrorContains(t, err, "Profile 2")
	require.ErrorContains(t, err, "Profile 5")
}

func TestPlanLaunchArgsAreAList(t *testing.T) {
	t.Parallel()
	chrome := AppEntry{ID: "exe:chrome", Name: "Google Chrome", Launch: AppLauncher{Method: LaunchExecutable, Path: userChrome}, Capabilities: []string{"launch", "browser_profiles"}, Profiles: []BrowserProfile{{"Profile 3", "Ömer Faruk"}}}
	plan, err := PlanLaunch(chrome, []string{"https://www.google.com/search?q=a b&c"}, "ömer faruk")
	require.NoError(t, err)
	require.Equal(t, []string{userChrome, "--profile-directory=Profile 3", "https://www.google.com/search?q=a b&c"}, plan.Argv)
	require.Equal(t, "chrome.exe", plan.ProcessName)

	_, err = PlanLaunch(chrome, []string{"--profile-directory=Default"}, "")
	require.ErrorIs(t, err, ErrLaunchArgs)
	_, err = PlanLaunch(chrome, []string{"a\x00b"}, "")
	require.ErrorIs(t, err, ErrLaunchArgs)

	calc := AppEntry{ID: "start:calc", Launch: AppLauncher{Method: LaunchAppsFolder, AppID: "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App"}}
	_, err = PlanLaunch(calc, []string{"x"}, "")
	require.ErrorIs(t, err, ErrLaunchArgs, "Start menu entries take no arguments")
	plan, err = PlanLaunch(calc, nil, "")
	require.NoError(t, err)
	require.Equal(t, `shell:AppsFolder\Microsoft.WindowsCalculator_8wekyb3d8bbwe!App`, plan.Argv[1])
}

// fakeLauncher records launches and serves scripted window listings.
type fakeLauncher struct {
	catalog  *AppCatalog
	launched []LaunchPlan
	pid      int
	before   []AppWindowObservation
	after    []AppWindowObservation
}

func (f *fakeLauncher) Apps() *AppCatalog { return f.catalog }

func (f *fakeLauncher) LaunchApp(_ context.Context, plan LaunchPlan) (int, error) {
	f.launched = append(f.launched, plan)
	return f.pid, nil
}

func (f *fakeLauncher) VisibleWindows(context.Context) ([]AppWindowObservation, error) {
	if len(f.launched) == 0 {
		return f.before, nil
	}
	return f.after, nil
}

func TestCatalogLaunchVerifiesTheNewWindow(t *testing.T) {
	t.Parallel()
	f := &fakeLauncher{
		catalog: fixtureCatalog(t, map[string]bool{userChrome: true}),
		pid:     4242,
		before:  []AppWindowObservation{{ID: "1", ProcessName: "explorer.exe", Foreground: true}},
		after:   []AppWindowObservation{{ID: "1", ProcessName: "explorer.exe"}, {ID: "77", Name: "Google", ProcessID: 4242, ProcessName: "chrome.exe", Foreground: true}},
	}
	raw, err := CatalogAutomation(t.Context(), f, AutomationRequest{Action: "launch_app", Name: "chrome", Args: []string{"https://www.google.com"}, Profile: "Ömer Faruk Aydın"})
	require.NoError(t, err)
	require.Len(t, f.launched, 1, "launched exactly once")
	require.Equal(t, []string{userChrome, "--profile-directory=Profile 3", "https://www.google.com"}, f.launched[0].Argv)
	var out struct {
		Verification string         `json:"verification"`
		Window       AppWindow      `json:"window"`
		Profile      BrowserProfile `json:"profile"`
		Verify       bool           `json:"verify_required"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, LaunchWindowObserved, out.Verification)
	require.Equal(t, "77", out.Window.WindowID)
	require.Equal(t, "Profile 3", out.Profile.Directory)
	require.False(t, out.Verify)
}

// Chrome hands a launch to its running process, which opens a tab in an
// existing window; that window becoming foreground is the evidence.
func TestCatalogLaunchAcceptsAReusedForegroundBrowserWindow(t *testing.T) {
	t.Parallel()
	f := &fakeLauncher{
		catalog: fixtureCatalog(t, map[string]bool{userChrome: true}),
		pid:     5000,
		before:  []AppWindowObservation{{ID: "198000", ProcessID: 14124, ProcessName: "chrome.exe"}, {ID: "1", ProcessName: "WindowsTerminal.exe", Foreground: true}},
		after:   []AppWindowObservation{{ID: "198000", ProcessID: 14124, ProcessName: "chrome.exe", Foreground: true}, {ID: "1", ProcessName: "WindowsTerminal.exe"}},
	}
	raw, err := CatalogAutomation(t.Context(), f, AutomationRequest{Action: "launch_app", AppID: "exe:chrome", Args: []string{"https://www.google.com"}})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"window_id":"198000"`)
	require.Contains(t, string(raw), LaunchWindowObserved)
}

func TestCatalogLaunchWithoutWindowIsNotRetried(t *testing.T) {
	t.Parallel()
	f := &fakeLauncher{catalog: fixtureCatalog(t, map[string]bool{userChrome: true}), pid: 1}
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	entry, err := f.catalog.Resolve(ctx, "chrome")
	require.NoError(t, err)
	plan, err := PlanLaunch(entry, nil, "")
	require.NoError(t, err)
	res, err := LaunchAndVerify(ctx, f, plan, 300*time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Started)
	require.Equal(t, LaunchNoWindowYet, res.Verification)
	require.Len(t, f.launched, 1)
}

func TestCatalogAppsListingIsBounded(t *testing.T) {
	t.Parallel()
	var found []AppEntry
	for i := range 80 {
		found = append(found, AppEntry{Name: "App", Launch: AppLauncher{Method: LaunchAppsFolder, AppID: "id" + string(rune('A'+i/26)) + string(rune('a'+i%26))}})
	}
	f := &fakeLauncher{catalog: NewAppCatalog(func(context.Context) ([]AppEntry, error) { return found, nil }, time.Minute)}
	raw, err := CatalogAutomation(t.Context(), f, AutomationRequest{Action: "apps"})
	require.NoError(t, err)
	var out struct {
		Total     int        `json:"total"`
		Truncated bool       `json:"truncated"`
		Apps      []AppEntry `json:"apps"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, 80, out.Total)
	require.True(t, out.Truncated)
	require.Len(t, out.Apps, 60)
}

func TestUsesCatalog(t *testing.T) {
	t.Parallel()
	require.True(t, UsesCatalog(AutomationRequest{Action: "apps"}))
	require.True(t, UsesCatalog(AutomationRequest{Action: "launch_app", AppID: "exe:chrome"}))
	require.True(t, UsesCatalog(AutomationRequest{Action: "launch_app", Name: "chrome", Profile: "x"}))
	require.False(t, UsesCatalog(AutomationRequest{Action: "launch_app", Name: "Calculator"}), "name-only keeps the Start menu path first")
}
