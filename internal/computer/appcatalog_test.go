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
