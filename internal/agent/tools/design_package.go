		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 4096 {
			return nil, fmt.Errorf("source URLs must be actual HTTPS references without embedded credentials")
		}
		for key := range u.Query() {
			key = strings.ToLower(key)
			if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || key == "key" || key == "code" || key == "signature" {
				return nil, fmt.Errorf("source URL contains a credential-like query parameter")
			}
		}
	}
	return slices.Clone(urls), nil
}

func loadPackageAudits(root string, ids []string, screens []deliveredScreen, sources []designSource, now time.Time) ([]designAudit, error) {
	if len(ids) > 100 {
		return nil, fmt.Errorf("at most 100 audit ids")
	}
	var audits []designAudit
	for _, id := range ids {
		if !validDesignAuditID(id) {
			return nil, fmt.Errorf("invalid design audit id")
		}
		path, err := transfer.Resolve(root, ".atlas/design/audits/"+id+".json")
		if err != nil {
			return nil, err
		}
		data, err := readDesignArtifact(path, 1<<20)
		if err != nil {
			return nil, err
		}
		var a designAudit
		if json.Unmarshal(data, &a) != nil || a.ID != id || a.CapturedAt <= 0 || time.UnixMilli(a.CapturedAt).After(now.Add(time.Minute)) || now.Sub(time.UnixMilli(a.CapturedAt)) > 24*time.Hour {
			return nil, fmt.Errorf("stale or invalid audit %s", id)
		}
		if !designAuditWasObserved(root, a) {
			return nil, fmt.Errorf("design audit %s is not current adapter evidence; re-audit after a restart or edited record", id)
		}
		if !slices.ContainsFunc(screens, func(s deliveredScreen) bool { return s.SHA256 == a.ScreenshotSHA256 }) {
			return nil, fmt.Errorf("audit %s does not describe any current delivered screen", id)
		}
		for _, s := range sources {
			if expected, ok := a.SourceHashes[s.Source]; ok && expected != s.SHA256 {
				return nil, fmt.Errorf("source changed since audit %s", id)
			}
		}
		audits = append(audits, a)
	}
	return audits, nil
}

func designPackageCompletion(m designManifest) string {
	if m.SystemStatus != "approved" || len(m.OpenQuestions) > 0 || len(m.RequiredScreens) == 0 || len(m.Sources) == 0 || len(m.Audits) == 0 || !m.QualityGate.Passed {
		return "partial"
	}
	for _, required := range m.RequiredScreens {
		if !slices.ContainsFunc(m.Screens, func(s deliveredScreen) bool { return s.Source == filepath.ToSlash(required) }) {
			return "partial"
		}
	}
	prototype := false
	for _, screen := range m.Screens {
		if !slices.ContainsFunc(m.Audits, func(a designAudit) bool { return a.Passed && a.ScreenshotSHA256 == screen.SHA256 }) {
			return "partial"
		}
	}
	for _, a := range m.Audits {
		for _, source := range m.Sources {
			if a.SourceHashes[source.Source] != source.SHA256 {
				return "partial"
			}
		}
		prototype = prototype || a.Passed && a.PrototypeSteps > 0 && a.AssertionCount > 0
	}
	if !prototype {
		return "partial"
	}
	return "complete"
}
