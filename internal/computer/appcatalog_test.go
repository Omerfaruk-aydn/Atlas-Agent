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
