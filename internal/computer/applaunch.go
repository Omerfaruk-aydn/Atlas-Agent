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
