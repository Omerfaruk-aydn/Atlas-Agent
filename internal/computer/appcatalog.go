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
