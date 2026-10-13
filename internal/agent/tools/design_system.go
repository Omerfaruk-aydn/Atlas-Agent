	m.QualityGate.Checks = append(m.QualityGate.Checks, DesignCheck{Rule: "design system approved by the user", Passed: status == "approved", Measured: fmt.Sprintf("version %d, %s", rec.Version, status), Required: "approved"})
	m.QualityGate.Checks = append(m.QualityGate.Checks, screenFrameChecks(sys, m.Screens)...)
	m.QualityGate.Passed = true
	for _, c := range m.QualityGate.Checks {
		m.QualityGate.Passed = m.QualityGate.Passed && c.Passed
	}
	m.Completion = designPackageCompletion(m)
	tmp, err := os.MkdirTemp(filepath.Join(root), ".design-delivery-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "screens"), 0o755); err != nil {
		return nil, err
	}
	for _, s := range screens {
		if err := os.WriteFile(filepath.Join(tmp, filepath.FromSlash(s.scr.File)), s.data, 0o644); err != nil {
			return nil, err
		}
	}
	if len(sources) > 0 {
		if err := os.MkdirAll(filepath.Join(tmp, "sources"), 0o755); err != nil {
			return nil, err
		}
	}
	for _, source := range sources {
		if err := os.WriteFile(filepath.Join(tmp, filepath.FromSlash(source.info.File)), source.data, 0o600); err != nil {
			return nil, err
		}
	}
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), manifest, 0o644); err != nil {
		return nil, err
	}
	sysData, _ := json.MarshalIndent(sys, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "design-system.json"), sysData, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte(deliveryReadme(m)), 0o644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	return map[string]any{"package": filepath.ToSlash(filepath.Join("design-delivery", p.Name)), "screens": len(m.Screens), "quality_gate": m.QualityGate, "completion": m.Completion, "delivery": "assembled_locally_not_presented", "visual_inspection_required": true}, nil
}

func readManifest(path string) (designManifest, bool) {
	var m designManifest
	data, err := readDesignArtifact(path, 4<<20)
	if err != nil {
		return m, false
	}
	return m, json.Unmarshal(data, &m) == nil
}

func sameDelivery(a, b designManifest) bool {
	if a.SystemVersion != b.SystemVersion || a.SystemStatus != b.SystemStatus || len(a.Screens) != len(b.Screens) {
		return false
	}
	left, _ := json.Marshal(a.System)
	right, _ := json.Marshal(b.System)
	if !bytes.Equal(left, right) || !slices.Equal(a.OpenQuestions, b.OpenQuestions) {
		return false
	}
	left, _ = json.Marshal([]any{a.Sources, a.SourceURLs, a.Audits, a.RequiredScreens})
	right, _ = json.Marshal([]any{b.Sources, b.SourceURLs, b.Audits, b.RequiredScreens})
	if !bytes.Equal(left, right) {
		return false
	}
	for i := range a.Screens {
		if a.Screens[i].SHA256 != b.Screens[i].SHA256 || a.Screens[i].File != b.Screens[i].File {
			return false
		}
	}
	return true
}

// screenFrameChecks compares delivered screen sizes with the platform: a
// phone design delivered as landscape web frames is caught here.
func screenFrameChecks(s DesignSystem, screens []deliveredScreen) []DesignCheck {
	if !designIsMobile(s.Platform) || s.Platform == "tablet" {
		return nil
	}
	var wrong []string
	for _, sc := range screens {
		if sc.Width >= sc.Height {
			wrong = append(wrong, fmt.Sprintf("%s %dx%d", sc.File, sc.Width, sc.Height))
		}
	}
	return []DesignCheck{{Rule: "screens are portrait phone frames", Passed: len(wrong) == 0, Measured: orStr(strings.Join(wrong, ", "), "all portrait"), Required: "height > width for " + s.Platform}}
}

func deliveryReadme(m designManifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", m.Name)
	fmt.Fprintf(&b, "- Product: %s\n- Platform: %s\n- Direction: %s (chosen by %s)\n- Design system: version %d (%s)\n", m.System.Product, m.System.Platform, m.System.Direction.Name, m.System.Direction.ChosenBy, m.SystemVersion, m.SystemStatus)
	if len(m.System.Direction.Alternatives) > 0 {
		fmt.Fprintf(&b, "- Also considered: %s\n", strings.Join(m.System.Direction.Alternatives, "; "))
	}
	fmt.Fprintf(&b, "- Created: %s\n\n", m.CreatedAt)
	fmt.Fprintf(&b, "- Completion: %s\n- Delivery: assembled locally; user presentation is not verified by packaging.\n\n", m.Completion)
	if m.Completion != "complete" {
		b.WriteString("This is a partial package: require a screen inventory, editable sources, observed DOM checks and observed prototype interactions before calling it complete.\n\n")
	}
	gate := "passed"
	if !m.QualityGate.Passed {
		gate = "FAILED"
	}
	fmt.Fprintf(&b, "## Quality gate: %s\n\n| Rule | Result | Measured | Required |\n|---|---|---|---|\n", gate)
	for _, c := range m.QualityGate.Checks {
		res := "pass"
		if !c.Passed {
			res = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.Rule, res, c.Measured, c.Required)
	}
	b.WriteString("\nContrast is computed from the color tokens. Visual quality of the screens was not machine-verified.\n\n## Screens\n\n")
	for _, s := range m.Screens {
		fmt.Fprintf(&b, "%d. `%s` — %dx%d, sha256 `%s`\n", s.Order, s.File, s.Width, s.Height, s.SHA256[:16])
	}
	b.WriteString("\n## Editable sources\n\n")
	for _, source := range m.Sources {
		fmt.Fprintf(&b, "- [%s](%s) — sha256 %s\n", source.Source, source.File, source.SHA256)
	}
	b.WriteString("\n## Original design/prototype links\n\nLinks are provided references, not proof that a prototype was exercised.\n\n")
	for _, url := range m.SourceURLs {
		fmt.Fprintf(&b, "- %s\n", url)
	}
	b.WriteString("\n## Observed audits\n\n")
	for _, audit := range m.Audits {
		fmt.Fprintf(&b, "- %s: %dx%d, passed=%t, overflow=%d, small targets=%d, low contrast=%d, unknown contrast=%d, prototype steps=%d.\n", audit.ID, audit.Width, audit.Height, audit.Passed, audit.Overflow, audit.SmallTargets, audit.LowContrast, audit.UnknownContrast, audit.PrototypeSteps)
	}
	b.WriteString("\n## Color tokens\n\n")
	names := make([]string, 0, len(m.System.Colors))
	for n := range m.System.Colors {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "- `%s`: %s\n", n, m.System.Colors[n])
	}
	if len(m.OpenQuestions) > 0 {
		b.WriteString("\n## Open questions\n\n")
		for _, q := range m.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	return b.String()
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "screen"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
