	}
	res.Started, res.ProcessID = true, pid
	verifyCtx, cancelVerify := context.WithTimeout(ctx, timeout)
	defer cancelVerify()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if ws, err := b.VisibleWindows(verifyCtx); err == nil {
			if w, ok := launchedWindow(ws, before, beforeForeground, plan.ProcessName, pid); ok {
				res.Verification = LaunchWindowObserved
				res.Window = &AppWindow{WindowID: w.ID, ProcessID: w.ProcessID, Title: w.Name}
				res.WaitedMS = time.Since(start).Milliseconds()
				return res, nil
			}
		}
		select {
		case <-ctx.Done():
			res.WaitedMS = time.Since(start).Milliseconds()
			return res, ctx.Err()
		case <-deadline.C:
			res.WaitedMS = time.Since(start).Milliseconds()
			return res, nil
		case <-tick.C:
		}
	}
}

// launchedWindow picks a window that appeared after the launch, preferring
// one of the launched program. A program that reuses an existing window
// (a browser opening a tab) counts only when that window became
// foreground after the launch.
func launchedWindow(ws []AppWindowObservation, before map[string]bool, beforeForeground, processName string, pid int) (AppWindowObservation, bool) {
	var fallback *AppWindowObservation
	for i, w := range ws {
		matches := processName != "" && strings.EqualFold(w.ProcessName, processName) || pid > 0 && int(w.ProcessID) == pid
		if !before[w.ID] && !w.Minimized && matches {
			return w, true
		}
		if before[w.ID] && w.Foreground && w.ID != beforeForeground && processName != "" && strings.EqualFold(w.ProcessName, processName) {
			fallback = &ws[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return AppWindowObservation{}, false
}

// CatalogLaunchWait bounds how long a catalog launch waits for its window.
const CatalogLaunchWait = 8 * time.Second

// UsesCatalog reports whether a request must be served by the application
// catalog instead of the Start menu name lookup: the apps listing, and
// launches that name an app_id, pass arguments or choose a profile.
func UsesCatalog(p AutomationRequest) bool {
	return p.Action == "apps" || p.Action == "launch_app" && (p.AppID != "" || len(p.Args) > 0 || p.Profile != "")
}

// CatalogAutomation serves the apps and catalog launch_app actions on the
// computer that runs the applications.
func CatalogAutomation(ctx context.Context, b AppLauncherBackend, p AutomationRequest) (json.RawMessage, error) {
	catalog := b.Apps()
	switch p.Action {
	case "apps":
		entries, err := catalog.List(ctx, p.Name, p.Refresh)
		if err != nil {
			return nil, err
		}
		const limit = 60
		total := len(entries)
		if total > limit {
			entries = entries[:limit]
		}
		return json.Marshal(map[string]any{
			"generation": catalog.Generation(),
			"total":      total,
			"truncated":  total > limit,
			"apps":       entries,
		})
	case "launch_app":
		target := p.AppID
		if target == "" {
			target = p.Name
		}
		if strings.TrimSpace(target) == "" {
			return nil, fmt.Errorf("%w: app_id or name required", ErrAppNotFound)
		}
		entry, err := catalog.Resolve(ctx, target)
		if err != nil {
			return nil, err
		}
		plan, err := PlanLaunch(entry, p.Args, p.Profile)
		if err != nil {
			return nil, err
		}
		res, err := LaunchAndVerify(ctx, b, plan, CatalogLaunchWait)
		if err != nil && !res.Started {
			return nil, err
		}
		out := map[string]any{
			"launch_requested": res.Started,
			"started":          res.Started,
			"application":      entry.Name,
			"application_id":   entry.ID,
			"verification":     res.Verification,
			"waited_ms":        res.WaitedMS,
			"verify_required":  res.Verification != LaunchWindowObserved,
		}
		if len(p.Args) > 0 {
			out["args"] = p.Args
		}
		if res.Profile != nil {
			out["profile"] = res.Profile
		}
		if res.Window != nil {
			out["window"] = res.Window
		}
		data, marshalErr := json.Marshal(out)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return data, err
	}
	return nil, fmt.Errorf("invalid_request: %s is not a catalog action", p.Action)
}
