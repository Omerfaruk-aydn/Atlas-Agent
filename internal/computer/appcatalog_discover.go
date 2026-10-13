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
