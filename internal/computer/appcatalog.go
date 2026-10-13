	var b strings.Builder
	space := true
	for _, r := range s {
		switch r {
		case 'İ', 'I', 'ı':
			r = 'i'
		case 'Ş', 'ş':
			r = 's'
		case 'Ğ', 'ğ':
			r = 'g'
		case 'Ü', 'ü':
			r = 'u'
		case 'Ö', 'ö':
			r = 'o'
		case 'Ç', 'ç':
			r = 'c'
		default:
			r = unicode.ToLower(r)
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// mergeEntries joins entries that launch the same executable (for example
// App Paths and a known install folder), assigns stable ids and sorts by
// name. Executables sharing a base name at different paths keep separate
// ids with a path suffix.
func mergeEntries(found []AppEntry) []AppEntry {
	byKey := map[string]int{}
	var out []AppEntry
	for _, e := range found {
		key := launchKey(e.Launch)
		if key == "" {
			continue
		}
		if i, ok := byKey[key]; ok {
			m := &out[i]
			for _, s := range e.Sources {
				if !slices.Contains(m.Sources, s) {
					m.Sources = append(m.Sources, s)
				}
			}
			for _, a := range append([]string{e.Name}, e.Aliases...) {
				if a != "" && a != m.Name && !slices.Contains(m.Aliases, a) {
					m.Aliases = append(m.Aliases, a)
				}
			}
			for _, w := range e.Running {
				if !slices.ContainsFunc(m.Running, func(r AppWindow) bool { return r.WindowID == w.WindowID }) {
					m.Running = append(m.Running, w)
				}
			}
			if len(m.Profiles) == 0 {
				m.Profiles = e.Profiles
			}
			m.Verified = m.Verified || e.Verified
			continue
		}
		byKey[key] = len(out)
		out = append(out, e)
	}
	// Stable ids. Executables are named by base name; a base name used by
	// several paths gets a short path hash.
	bases := map[string]int{}
	for _, e := range out {
		if e.Launch.Method == LaunchExecutable {
			bases[exeBase(e.Launch.Path)]++
		}
	}
	for i := range out {
		e := &out[i]
		switch e.Launch.Method {
		case LaunchExecutable:
			base := exeBase(e.Launch.Path)
			e.ID = "exe:" + base
			if bases[base] > 1 {
				sum := sha256.Sum256([]byte(strings.ToLower(e.Launch.Path)))
				e.ID += "#" + hex.EncodeToString(sum[:3])
			}
		case LaunchAppsFolder:
			e.ID = "start:" + e.Launch.AppID
		}
		e.Aliases = slices.DeleteFunc(slices.Clone(e.Aliases), func(a string) bool { return a == e.Name })
		sort.Strings(e.Aliases)
		e.Aliases = slices.Compact(e.Aliases)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if FoldName(out[i].Name) != FoldName(out[j].Name) {
			return FoldName(out[i].Name) < FoldName(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func launchKey(l AppLauncher) string {
	switch l.Method {
	case LaunchExecutable:
		if l.Path == "" {
			return ""
		}
		return "exe|" + strings.ToLower(filepath.Clean(l.Path))
	case LaunchAppsFolder:
		if l.AppID == "" {
			return ""
		}
		return "start|" + strings.ToLower(l.AppID)
	}
	return ""
}

func exeBase(path string) string {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(path, `\`, "/")))
	return strings.TrimSuffix(base, ".exe")
}

// LaunchPlan is a structured launch: the program and its literal argv.
type LaunchPlan struct {
	Entry   AppEntry        `json:"app"`
	Argv    []string        `json:"argv"`
	Profile *BrowserProfile `json:"profile,omitempty"`
	// ProcessName identifies the windows that verify the launch.
	ProcessName string `json:"process_name,omitempty"`
}

// PlanLaunch builds the argv for an entry. args are literal arguments;
// profile is a browser profile display name or directory, resolved against
// the entry's discovered profiles. Nothing is joined into shell text.
func PlanLaunch(entry AppEntry, args []string, profile string) (LaunchPlan, error) {
	if len(args) > 32 {
		return LaunchPlan{}, fmt.Errorf("%w: at most 32 arguments", ErrLaunchArgs)
	}
	for _, a := range args {
		if strings.ContainsRune(a, 0) || len(a) > 4096 {
			return LaunchPlan{}, fmt.Errorf("%w: arguments must be at most 4096 bytes without NUL", ErrLaunchArgs)
		}
		if strings.HasPrefix(strings.ToLower(a), "--profile-directory") {
			return LaunchPlan{}, fmt.Errorf("%w: choose a profile with profile, not a raw --profile-directory argument", ErrLaunchArgs)
		}
	}
	plan := LaunchPlan{Entry: entry}
	switch entry.Launch.Method {
	case LaunchExecutable:
		plan.Argv = append([]string{entry.Launch.Path}, args...)
		plan.ProcessName = filepath.Base(strings.ReplaceAll(entry.Launch.Path, `\`, "/"))
	case LaunchAppsFolder:
		if len(args) > 0 || profile != "" {
			return LaunchPlan{}, fmt.Errorf("%w: %s is a Start menu entry and accepts no arguments or profile", ErrLaunchArgs, entry.Name)
		}
		plan.Argv = []string{`explorer.exe`, `shell:AppsFolder\` + entry.Launch.AppID}
	default:
		return LaunchPlan{}, fmt.Errorf("%w: unknown launch method %q", ErrLaunchArgs, entry.Launch.Method)
	}
	if profile != "" {
		if !slices.Contains(entry.Capabilities, "browser_profiles") {
			return LaunchPlan{}, fmt.Errorf("%w: %s has no browser profiles", ErrLaunchArgs, entry.Name)
		}
		p, err := MatchProfile(entry.Profiles, profile)
		if err != nil {
			return LaunchPlan{}, err
		}
		plan.Profile = &p
		plan.Argv = append([]string{plan.Argv[0], "--profile-directory=" + p.Directory}, plan.Argv[1:]...)
	}
	return plan, nil
}

// MatchProfile resolves a profile by directory, exact display name, or a
// unique token match ("ömer faruk aydın" finds the profile "ömer faruk").
// Several candidates are reported instead of guessed.
func MatchProfile(profiles []BrowserProfile, want string) (BrowserProfile, error) {
	for _, p := range profiles {
		if p.Directory == want {
			return p, nil
		}
	}
	q := FoldName(want)
	var exact, partial []BrowserProfile
	for _, p := range profiles {
		f := FoldName(p.Name)
		switch {
		case f == q:
			exact = append(exact, p)
		case tokenSubset(q, f):
			partial = append(partial, p)
		}
	}
	pick := exact
	if len(pick) == 0 {
		pick = closestProfiles(partial, q)
	}
	switch len(pick) {
	case 1:
		return pick[0], nil
	case 0:
		names := make([]string, 0, len(profiles))
		for _, p := range profiles {
			names = append(names, p.Name)
		}
		return BrowserProfile{}, fmt.Errorf("%w: no profile matches %q; profiles: %s", ErrProfileNotFound, want, strings.Join(names, ", "))
	}
	names := make([]string, 0, len(pick))
	for _, p := range pick {
		names = append(names, fmt.Sprintf("%s (%s)", p.Name, p.Directory))
	}
	return BrowserProfile{}, fmt.Errorf("%w: %q matches %s; ask which one", ErrProfileAmbig, want, strings.Join(names, ", "))
}

// closestProfiles keeps the partial matches sharing the most words with q:
// "ömer faruk aydın" prefers "Ömer Faruk" over "Ömer". Equal overlap stays
// ambiguous.
func closestProfiles(partial []BrowserProfile, q string) []BrowserProfile {
	best, out := 0, []BrowserProfile(nil)
	for _, p := range partial {
		n := 0
		for _, t := range strings.Fields(FoldName(p.Name)) {
			if slices.Contains(strings.Fields(q), t) {
				n++
			}
		}
		switch {
		case n > best:
			best, out = n, []BrowserProfile{p}
		case n == best:
			out = append(out, p)
		}
	}
	return out
}
