	Exists    func(string) bool
	ReadFile  func(string) ([]byte, error)
	StartApps func(context.Context) ([]startApp, error)
	Windows   func(context.Context) ([]nativeWindowSummary, error)
}

// nativeWindowSummary is the part of a window observation discovery uses.
type nativeWindowSummary struct {
	ID          string `json:"window_id"`
	Name        string `json:"name"`
	ProcessID   uint32 `json:"process_id"`
	ProcessName string `json:"process_name"`
}

// knownApp describes applications Atlas recognizes by executable name: a
// display name, localized aliases, well-known install folders and, for
// Chromium browsers, where profile names live.
type knownApp struct {
	Exe      string
	Name     string
	Aliases  []string
	Browser  bool
	UserData string   // Relative to %LOCALAPPDATA%; Chromium only.
	Folders  []string // "%VAR%\rest" install locations.
	Chromium bool
}

var knownApps = []knownApp{
	{Exe: "chrome.exe", Name: "Google Chrome", Aliases: []string{"Chrome", "Google Chrome", "Krom"}, Browser: true, Chromium: true, UserData: `Google\Chrome\User Data`, Folders: []string{`%ProgramFiles%\Google\Chrome\Application\chrome.exe`, `%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe`, `%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe`}},
	{Exe: "msedge.exe", Name: "Microsoft Edge", Aliases: []string{"Edge"}, Browser: true, Chromium: true, UserData: `Microsoft\Edge\User Data`, Folders: []string{`%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe`, `%ProgramFiles%\Microsoft\Edge\Application\msedge.exe`}},
	{Exe: "brave.exe", Name: "Brave", Aliases: []string{"Brave Browser"}, Browser: true, Chromium: true, UserData: `BraveSoftware\Brave-Browser\User Data`, Folders: []string{`%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`, `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\Application\brave.exe`}},
	{Exe: "firefox.exe", Name: "Mozilla Firefox", Aliases: []string{"Firefox"}, Browser: true, Folders: []string{`%ProgramFiles%\Mozilla Firefox\firefox.exe`, `%ProgramFiles(x86)%\Mozilla Firefox\firefox.exe`}},
}

// localizedStartAliases adds the names an application has in other Windows
// display languages, keyed by its AppUserModelID.
var localizedStartAliases = map[string][]string{
	"Microsoft.WindowsCalculator_8wekyb3d8bbwe!App":                                       {"Calculator", "Hesap Makinesi", "Rechner", "Calculatrice", "Calcolatrice", "الحاسبة"},
	"Microsoft.WindowsNotepad_8wekyb3d8bbwe!App":                                          {"Notepad", "Not Defteri", "Editor", "Bloc-notes", "Blocco note"},
	`{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\notepad.exe`:                                  {"Notepad", "Not Defteri", "Editor", "Bloc-notes", "Blocco note"},
	"Microsoft.Windows.Explorer":                                                          {"File Explorer", "Explorer", "Windows Explorer", "Dosya Gezgini", "Datei-Explorer", "Explorateur de fichiers", "Esplora file", "Explorador de archivos"},
	"windows.immersivecontrolpanel_cw5n1h2txyewy!microsoft.windows.immersivecontrolpanel": {"Settings", "Ayarlar", "Einstellungen", "Paramètres"},
	"Microsoft.Paint_8wekyb3d8bbwe!App":                                                   {"Paint"},
}

func knownByExe(exe string) (knownApp, bool) {
	for _, k := range knownApps {
		if strings.EqualFold(k.Exe, exe) {
			return k, true
		}
	}
	return knownApp{}, false
}

func expandFolder(pattern string, env func(string) string) (path, scope string, ok bool) {
	if !strings.HasPrefix(pattern, "%") {
		return pattern, "system", true
	}
	end := strings.Index(pattern[1:], "%")
	if end < 0 {
		return "", "", false
	}
	name := pattern[1 : end+1]
	value := env(name)
	if value == "" {
		return "", "", false
	}
	scope = "system"
	if strings.EqualFold(name, "LOCALAPPDATA") || strings.EqualFold(name, "APPDATA") {
		scope = "user"
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
