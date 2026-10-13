	}
	return value + pattern[end+2:], scope, true
}

// discoverApps assembles the catalog from all sources. A source that fails
// contributes nothing; the others still count.
func discoverApps(ctx context.Context, src appSources) ([]AppEntry, error) {
	var out []AppEntry
	addExe := func(path, scope, source string) {
		exe := filepath.Base(strings.ReplaceAll(path, `\`, "/"))
		if !strings.EqualFold(filepath.Ext(exe), ".exe") || !src.Exists(path) {
			return
		}
		e := AppEntry{
			Name:         strings.TrimSuffix(exe, filepath.Ext(exe)),
			Launch:       AppLauncher{Method: LaunchExecutable, Path: path},
			Sources:      []string{source},
			Scope:        scope,
			Verified:     true,
			Capabilities: []string{"launch", "launch_args", "window_focus"},
		}
		if k, ok := knownByExe(exe); ok {
			e.Name, e.Aliases = k.Name, append([]string{strings.TrimSuffix(k.Exe, ".exe")}, k.Aliases...)
			if k.Browser {
				e.Capabilities = append(e.Capabilities, "browser")
			}
			if k.Chromium {
				e.Capabilities = append(e.Capabilities, "browser_profiles")
				e.Profiles = chromiumProfiles(src, k)
			}
		}
		out = append(out, e)
	}
	if src.AppPaths != nil {
		for _, v := range src.AppPaths() {
			addExe(strings.Trim(v.Path, `"`), v.Scope, "app_paths_"+v.Scope)
		}
	}
	for _, k := range knownApps {
		for _, f := range k.Folders {
			if path, scope, ok := expandFolder(f, src.Env); ok {
				addExe(path, scope, "known_location")
			}
		}
	}
	if src.StartApps != nil {
		if apps, err := src.StartApps(ctx); err == nil {
			for _, a := range apps {
				if a.AppID == "" || a.Name == "" {
					continue
				}
				if i := startShortcutOf(out, a); i >= 0 {
					// A Start menu shortcut to a discovered program is the
					// same application, not a second candidate.
					if !slices.Contains(out[i].Sources, "start_menu") {
						out[i].Sources = append(out[i].Sources, "start_menu")
					}
					continue
				}
				out = append(out, AppEntry{
					Name:         a.Name,
					Aliases:      slices.Clone(localizedStartAliases[a.AppID]),
					Launch:       AppLauncher{Method: LaunchAppsFolder, AppID: a.AppID},
					Sources:      []string{"start_menu"},
					Scope:        "unknown",
					Verified:     true,
					Capabilities: []string{"launch", "window_focus"},
				})
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src.Windows != nil {
		if windows, err := src.Windows(ctx); err == nil {
			for i := range out {
				if out[i].Launch.Method != LaunchExecutable {
					continue
				}
				exe := filepath.Base(strings.ReplaceAll(out[i].Launch.Path, `\`, "/"))
				for _, w := range windows {
					if strings.EqualFold(w.ProcessName, exe) {
						out[i].Running = append(out[i].Running, AppWindow{WindowID: w.ID, ProcessID: w.ProcessID, Title: w.Name})
					}
				}
			}
		}
	}
	return out, nil
}

// startShortcutOf returns the executable entry a Start menu entry starts:
// same display name, or an AppUserModelID that is that program's path.
func startShortcutOf(entries []AppEntry, a startApp) int {
	for i, e := range entries {
		if e.Launch.Method != LaunchExecutable {
			continue
		}
		exe := strings.ToLower(filepath.Base(strings.ReplaceAll(e.Launch.Path, `\`, "/")))
		id := strings.ToLower(a.AppID)
		if FoldName(e.Name) == FoldName(a.Name) || id == strings.ToLower(e.Launch.Path) || strings.HasSuffix(id, `\`+exe) {
			return i
		}
	}
	return -1
}

// chromiumProfiles reads profile directory names and display names from a
// Chromium "Local State" file. Only profile.info_cache names are read;
// account e-mail, cookies, passwords and tokens are ignored.
func chromiumProfiles(src appSources, k knownApp) []BrowserProfile {
	if src.ReadFile == nil || src.Env == nil || k.UserData == "" {
		return nil
	}
	base := src.Env("LOCALAPPDATA")
	if base == "" {
		return nil
	}
	data, err := src.ReadFile(base + `\` + k.UserData + `\Local State`)
	if err != nil || len(data) > 16<<20 {
		return nil
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	var out []BrowserProfile
	for dir, info := range state.Profile.InfoCache {
		if dir == "" || strings.ContainsAny(dir, `/\`) {
			continue
		}
		out = append(out, BrowserProfile{Directory: dir, Name: info.Name})
	}
	sortProfiles(out)
	return out
}

func sortProfiles(p []BrowserProfile) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].Directory < p[j-1].Directory; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}
