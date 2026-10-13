	if _, err := transfer.Resolve(key, designSystemPath); err != nil {
		return nil, fmt.Errorf("design memory path: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(key)))
	mu := &designStoreLocks[int(sum[0])%len(designStoreLocks)]
	mu.Lock()
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lockPath, err := transfer.Resolve(key, designSystemPath+".lock")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	lockCtx, cancelLock := context.WithTimeout(ctx, 5*time.Second)
	defer cancelLock()
	release, err := lock.File(lockCtx, lockPath)
	if err != nil {
		return nil, fmt.Errorf("design memory lock: %w", err)
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stamp := func() string { return now().UTC().Format(time.RFC3339) }
	switch p.Action {
	case "get":
		st, err := loadDesignStore(dir)
		if errors.Is(err, os.ErrNotExist) || (err == nil && st.Approved == nil && st.Candidate == nil) {
			return map[string]any{"exists": false, "history_versions": st.versions(), "note": "No design system yet. Ask the user for the platform (phone, tablet, web...) and offer 2-3 visual directions before generating anything; then store the choice with set."}, nil
		}
		if err != nil {
			return nil, err
		}
		out := map[string]any{"exists": true, "path": designSystemPath, "history_versions": st.versions()}
		if st.MigratedFrom != 0 {
			out["migrated_from_schema"] = st.MigratedFrom
		}
		if st.Approved != nil {
			out["approved"] = st.Approved
		}
		if st.Candidate != nil {
			out["candidate"] = st.Candidate
			out["note"] = "The candidate is not approved yet: show it to the user and call approve only after they explicitly approve it."
		}
		eff, status := st.effective()
		out["status"] = status
		out["system"] = eff.System
		out["check"] = checkDesignSystem(eff.System)
		return out, nil
	case "set":
		if p.System == nil {
			return nil, errors.New("set needs system")
		}
		sys := *p.System
		if err := validateDesignSystem(sys); err != nil {
			return nil, err
		}
		st, backup, err := loadDesignStoreForWrite(dir, now)
		if err != nil {
			return nil, err
		}
		sys.UpdatedAt = stamp()
		st.Candidate = &designRecord{Version: st.nextVersion(), System: sys, At: sys.UpdatedAt}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := saveDesignStore(dir, st); err != nil {
			return nil, err
		}
		out := map[string]any{"saved": designSystemPath, "status": "candidate", "version": st.Candidate.Version, "check": checkDesignSystem(sys)}
		if st.Approved != nil {
			out["note"] = fmt.Sprintf("Stored as a candidate; approved version %d stays in use until the user approves this one (approve).", st.Approved.Version)
		} else {
			out["note"] = "Stored as a candidate. Show it to the user and call approve once they explicitly approve it."
		}
		if backup != "" {
			out["corrupt_backup"] = backup
		}
		return out, nil
	case "approve":
		st, err := loadDesignStore(dir)
		if err != nil {
			return nil, fmt.Errorf("approve needs a stored candidate: %w", err)
		}
		if st.Candidate == nil {
			return nil, errors.New("approve needs a candidate: store the system the user approved with set first")
		}
		if p.ExpectedVersion > 0 && p.ExpectedVersion != st.Candidate.Version {
			return nil, errors.New("stale_design_revision: the candidate changed; show the new candidate before approval")
		}
		if st.Approved != nil {
			st.pushHistory(*st.Approved)
		}
		rec := *st.Candidate
		rec.At, rec.Note = stamp(), strings.TrimSpace(p.Note)
		st.Approved, st.Candidate = &rec, nil
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := saveDesignStore(dir, st); err != nil {
			return nil, err
		}
		return map[string]any{"approved": rec.Version, "path": designSystemPath}, nil
	case "reset":
		st, backup, err := loadDesignStoreForWrite(dir, now)
		if err != nil {
			return nil, err
		}
		if st.Approved == nil && st.Candidate == nil && backup == "" {
			return map[string]any{"reset": false, "note": "Nothing was stored."}, nil
		}
		if st.Approved != nil {
			rec := *st.Approved
			rec.Note = strings.Trim(rec.Note+"; reset: "+strings.TrimSpace(p.Note), "; ")
			st.pushHistory(rec)
		}
		st.Approved, st.Candidate = nil, nil
		if err := saveDesignStore(dir, st); err != nil {
			return nil, err
		}
		out := map[string]any{"reset": true, "history_versions": st.versions()}
		if backup != "" {
			out["corrupt_backup"] = backup
		}
		return out, nil
	case "history":
		st, err := loadDesignStore(dir)
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{"history": []designRecord{}}, nil
		}
		if err != nil {
			return nil, err
		}
		recs := slices.Clone(st.History)
		if st.Approved != nil {
			recs = append(recs, *st.Approved)
		}
		return map[string]any{"history": recs}, nil
	case "check":
		sys := p.System
		if sys == nil {
			st, err := loadDesignStore(dir)
			if err == nil && st.Approved == nil && st.Candidate == nil {
				err = os.ErrNotExist
			}
			if err != nil {
				return nil, fmt.Errorf("check needs system or a stored design system: %w", err)
			}
			eff, _ := st.effective()
			sys = &eff.System
		}
		if err := validateDesignSystem(*sys); err != nil {
			return nil, err
		}
		return checkDesignSystem(*sys), nil
	case "package":
		return packageDesign(ctx, dir, p, now)
	}
	return nil, fmt.Errorf("unknown action %q", p.Action)
}

// Design memory store. Schema 2 keeps the user-approved system apart from
// the latest candidate and keeps earlier approved versions. Schema 1 (a
// bare DesignSystem object) is migrated on read and rewritten as schema 2
// on the next write.
const (
	designStoreSchema = 2
	designHistoryCap  = 20
)

var designStoreLocks [64]sync.Mutex

var errDesignCorrupt = errors.New("design memory is damaged")

type designRecord struct {
	Version int          `json:"version"`
	System  DesignSystem `json:"system"`
	At      string       `json:"at"`
	Note    string       `json:"note,omitempty"`
}

type designStore struct {
	SchemaVersion int            `json:"schema_version"`
	Approved      *designRecord  `json:"approved,omitempty"`
	Candidate     *designRecord  `json:"candidate,omitempty"`
	History       []designRecord `json:"history,omitempty"`
	MigratedFrom  int            `json:"migrated_from,omitempty"`
}

// effective is the system later work follows: the approved one, else the
// candidate.
func (s designStore) effective() (designRecord, string) {
	if s.Approved != nil {
		return *s.Approved, "approved"
	}
	if s.Candidate != nil {
		return *s.Candidate, "candidate"
	}
	return designRecord{}, "none"
}

func (s designStore) nextVersion() int {
	n := 0
	for _, r := range s.History {
		n = max(n, r.Version)
	}
	if s.Approved != nil {
		n = max(n, s.Approved.Version)
	}
	if s.Candidate != nil {
		n = max(n, s.Candidate.Version)
	}
	return n + 1
}

func (s *designStore) pushHistory(r designRecord) {
	s.History = append(s.History, r)
	if over := len(s.History) - designHistoryCap; over > 0 {
		s.History = slices.Delete(s.History, 0, over)
	}
}

func (s designStore) versions() []int {
	out := []int{}
	for _, r := range s.History {
		out = append(out, r.Version)
	}
	if s.Approved != nil {
		out = append(out, s.Approved.Version)
	}
	return out
}

func designStoreFile(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(designSystemPath))
}

// loadDesignStore reads the project's design memory. A missing file is
// os.ErrNotExist; an unreadable or invalid one is errDesignCorrupt and is
// never treated as an empty memory.
func loadDesignStore(dir string) (designStore, error) {
	data, err := readDesignArtifact(designStoreFile(dir), 4<<20)
	if err != nil {
		return designStore{}, err
	}
	var probe struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return designStore{}, fmt.Errorf("%w: %s is not valid JSON (%v); it was left untouched. Do not assume a platform or direction: ask the user, then store a new candidate with set (the damaged file is kept as a backup)", errDesignCorrupt, designSystemPath, err)
	}
	if probe.SchemaVersion == nil {
		var legacy DesignSystem
		if err := json.Unmarshal(data, &legacy); err != nil {
			return designStore{}, fmt.Errorf("%w: %s: %v", errDesignCorrupt, designSystemPath, err)
		}
		if err := validateDesignSystem(legacy); err != nil {
			return designStore{}, fmt.Errorf("%w: %s: %v", errDesignCorrupt, designSystemPath, err)
		}
		// Schema 1 was written only after the user chose the platform
		// and direction, so it is migrated as approved version 1.
		return designStore{
			SchemaVersion: designStoreSchema,
			Approved:      &designRecord{Version: 1, System: legacy, At: legacy.UpdatedAt, Note: "migrated from schema 1"},
			MigratedFrom:  1,
		}, nil
	}
	if *probe.SchemaVersion > designStoreSchema {
		return designStore{}, fmt.Errorf("%s has schema %d from a newer Atlas; it was left untouched", designSystemPath, *probe.SchemaVersion)
	}
	if *probe.SchemaVersion != designStoreSchema {
		return designStore{}, fmt.Errorf("%w: unsupported design schema %d", errDesignCorrupt, *probe.SchemaVersion)
	}
	var st designStore
	if err := json.Unmarshal(data, &st); err != nil {
		return designStore{}, fmt.Errorf("%w: %s: %v", errDesignCorrupt, designSystemPath, err)
	}
	if len(st.History) > designHistoryCap {
		return designStore{}, fmt.Errorf("%w: design history exceeds %d versions", errDesignCorrupt, designHistoryCap)
	}
	records := []*designRecord{st.Approved, st.Candidate}
	for i := range st.History {
		records = append(records, &st.History[i])
	}
	seen := map[int]bool{}
	for _, r := range records {
		if r == nil {
			continue
		}
		if r.Version <= 0 || seen[r.Version] {
			return designStore{}, fmt.Errorf("%w: invalid or duplicate design version %d", errDesignCorrupt, r.Version)
		}
		seen[r.Version] = true
		if err := validateDesignSystem(r.System); err != nil {
			return designStore{}, fmt.Errorf("%w: %s version %d: %v", errDesignCorrupt, designSystemPath, r.Version, err)
		}
	}
	return st, nil
}

// loadDesignStoreForWrite returns the store a write builds on. A damaged
// file is moved aside (its backup path is returned) instead of being
// silently overwritten; a newer schema is refused.
func loadDesignStoreForWrite(dir string, now func() time.Time) (designStore, string, error) {
	st, err := loadDesignStore(dir)
	switch {
	case err == nil:
		return st, "", nil
	case errors.Is(err, os.ErrNotExist):
		return designStore{SchemaVersion: designStoreSchema}, "", nil
	case errors.Is(err, errDesignCorrupt):
		base := designStoreFile(dir) + ".corrupt-" + now().UTC().Format("20060102T150405Z")
		backup := base
		for i := 0; ; i++ {
			_, statErr := os.Lstat(backup)
			if errors.Is(statErr, os.ErrNotExist) {
				break
			}
			if statErr != nil {
				return designStore{}, "", statErr
			}
			if i >= 1000 {
				return designStore{}, "", errors.New("too many corrupt design backups for this timestamp")
			}
			backup = fmt.Sprintf("%s-%d", base, i+1)
		}
		if rerr := os.Rename(designStoreFile(dir), backup); rerr != nil {
			return designStore{}, "", fmt.Errorf("%w; moving it aside failed: %v", err, rerr)
		}
		rel, _ := filepath.Rel(dir, backup)
		return designStore{SchemaVersion: designStoreSchema}, filepath.ToSlash(rel), nil
	default:
		return designStore{}, "", err
	}
}

func saveDesignStore(dir string, st designStore) error {
	st.SchemaVersion = designStoreSchema
	st.MigratedFrom = 0
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return taskstate.WriteFileAtomic(designStoreFile(dir), data, 0o644)
}

// validateDesignSystem rejects an incomplete memory: one that would let a
// later session guess the platform or the direction again.
func validateDesignSystem(s DesignSystem) error {
	var problems []string
	if strings.TrimSpace(s.Product) == "" {
		problems = append(problems, "product is required")
	}
	if !slices.Contains(designPlatforms, s.Platform) {
		problems = append(problems, "platform must be one of "+strings.Join(designPlatforms, ", ")+"; ask the user when it is not clear")
	}
	if strings.TrimSpace(s.Direction.Name) == "" || (s.Direction.ChosenBy != "user" && s.Direction.ChosenBy != "default") {
		problems = append(problems, "direction.name and direction.chosen_by (user or default) are required; offer directions and let the user choose")
	}
	if len(s.Colors) == 0 || len(s.Colors) > 64 {
		problems = append(problems, "colors needs 1-64 tokens")
	}
	for name, v := range s.Colors {
		if !hexColor.MatchString(v) {
			problems = append(problems, fmt.Sprintf("color %s must be #RRGGBB, got %q", name, v))
		}
	}
	if len(s.Pairs) == 0 {
		problems = append(problems, "pairs must list each text/icon color on each background it is used on")
	}
	for _, pr := range s.Pairs {
		if _, ok := s.Colors[pr.Foreground]; !ok {
			problems = append(problems, "pair foreground "+pr.Foreground+" is not a color token")
		}
		if _, ok := s.Colors[pr.Background]; !ok {
			problems = append(problems, "pair background "+pr.Background+" is not a color token")
		}
		if !slices.Contains([]string{"text", "large_text", "ui"}, pr.Usage) {
			problems = append(problems, "pair usage must be text, large_text or ui")
		}
	}
	if designIsMobile(s.Platform) && s.SafeArea == nil {
		problems = append(problems, "safe_area is required for "+s.Platform)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New("invalid design system: " + strings.Join(problems, "; "))
	}
	return nil
}

// checkDesignSystem measures what can be measured. Contrast ratios are
// computed from the tokens (WCAG 2.x relative luminance), not judged.
func checkDesignSystem(s DesignSystem) designReport {
	var checks []DesignCheck
	for _, pr := range s.Pairs {
		fg, err1 := parseHexColor(s.Colors[pr.Foreground])
		bg, err2 := parseHexColor(s.Colors[pr.Background])
		if err1 != nil || err2 != nil {
			continue
		}
		ratio := contrastRatio(fg, bg)
		need := 4.5
		if pr.Usage != "text" {
			need = 3
		}
		checks = append(checks, DesignCheck{
			Rule:     fmt.Sprintf("contrast %s on %s (%s)", pr.Foreground, pr.Background, pr.Usage),
			Passed:   ratio >= need,
			Measured: strconv.FormatFloat(math.Floor(ratio*100)/100, 'f', 2, 64) + ":1",
			Required: strconv.FormatFloat(need, 'f', 1, 64) + ":1",
			Ratio:    ratio,
		})
	}
	if designIsMobile(s.Platform) {
		if s.SafeArea != nil {
			checks = append(checks, DesignCheck{Rule: "safe area declared", Passed: s.SafeArea.Top > 0 && s.SafeArea.Bottom > 0, Measured: fmt.Sprintf("top %d, bottom %d", s.SafeArea.Top, s.SafeArea.Bottom), Required: "top and bottom insets > 0"})
		}
		minTouch := 44
		if s.Platform == "android" {
			minTouch = 48
		}
		checks = append(checks, DesignCheck{Rule: "touch target size", Passed: s.MinTouch >= minTouch, Measured: strconv.Itoa(s.MinTouch), Required: ">= " + strconv.Itoa(minTouch)})
		phone := false
		for _, v := range s.Viewports {
			if v.Width >= 320 && v.Width <= 480 && v.Height > v.Width {
				phone = true
			}
		}
		if s.Platform != "tablet" {
			checks = append(checks, DesignCheck{Rule: "phone portrait viewport", Passed: phone, Measured: viewportList(s.Viewports), Required: "a portrait frame 320-480 wide"})
		}
	}
	if len(s.TypeScale) > 0 {
		checks = append(checks, DesignCheck{Rule: "type scale ascending", Passed: slices.IsSorted(s.TypeScale) && s.TypeScale[0] >= 11, Measured: intList(s.TypeScale), Required: "ascending, smallest >= 11"})
	}
	report := designReport{Passed: true, Checks: checks}
	for _, c := range checks {
		report.Passed = report.Passed && c.Passed
	}
	return report
}

func viewportList(vs []DesignViewport) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, fmt.Sprintf("%s %dx%d", v.Name, v.Width, v.Height))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func intList(v []int) string {
	parts := make([]string, 0, len(v))
	for _, n := range v {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ", ")
}

type rgb struct{ R, G, B uint8 }

func parseHexColor(s string) (rgb, error) {
	if !hexColor.MatchString(s) {
		return rgb{}, fmt.Errorf("not #RRGGBB: %q", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return rgb{}, err
	}
	return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

func relativeLuminance(c rgb) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// contrastRatio is the WCAG 2.x contrast ratio, 1 to 21.
func contrastRatio(a, b rgb) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

type deliveredScreen struct {
	Order  int    `json:"order"`
	File   string `json:"file"`
	Source string `json:"source"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type designManifest struct {
	Name            string            `json:"name"`
	CreatedAt       string            `json:"created_at"`
	SystemVersion   int               `json:"system_version"`
	SystemStatus    string            `json:"system_status"`
	System          DesignSystem      `json:"system"`
	QualityGate     designReport      `json:"quality_gate"`
	Screens         []deliveredScreen `json:"screens"`
	OpenQuestions   []string          `json:"open_questions,omitempty"`
	Sources         []designSource    `json:"sources,omitempty"`
	SourceURLs      []string          `json:"source_urls,omitempty"`
	Audits          []designAudit     `json:"audits,omitempty"`
	RequiredScreens []string          `json:"required_screens,omitempty"`
	Completion      string            `json:"completion"`
}

type designSource struct {
	File   string `json:"file"`
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

var deliveryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// packageDesign assembles design-delivery/<name>: the screens (verified
// images, hashed), the design system, the measured quality gate and a
// README. Failures are reported in the package, never hidden.
func packageDesign(ctx context.Context, dir string, p DesignSystemParams, now func() time.Time) (any, error) {
	if !deliveryName.MatchString(p.Name) {
		return nil, errors.New("package needs name: letters, digits, dot, dash or underscore, at most 64")
	}
	if len(p.Screens) == 0 || len(p.Screens) > 100 {
		return nil, errors.New("package needs 1-100 screens")
	}
	st, err := loadDesignStore(dir)
	if err == nil && st.Approved == nil && st.Candidate == nil {
		err = os.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("package needs a stored design system (design_system set): %w", err)
	}
	rec, status := st.effective()
	sys := rec.System
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	out, err := transfer.Resolve(root, "design-delivery/"+p.Name)
	if err != nil {
		return nil, err
	}
	type staged struct {
		data []byte
		scr  deliveredScreen
	}
	var screens []staged
	totalBytes := 0
	for i, rel := range p.Screens {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		src, err := transfer.Resolve(root, rel)
		if err != nil {
			return nil, fmt.Errorf("screen %q must be inside the working directory", rel)
		}
		data, err := readDesignArtifact(src, 64<<20)
		if err != nil {
			return nil, fmt.Errorf("screen %q: %w", rel, err)
		}
		totalBytes += len(data)
		if totalBytes > 128<<20 {
			return nil, errors.New("design package exceeds 128 MiB total")
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("screen %q is not a readable image: %w", rel, err)
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxImageDeliveryPixels {
			return nil, fmt.Errorf("screen %q exceeds the bounded image pixel limit", rel)
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("screen %q is a damaged %s image: %w", rel, format, err)
		}
		sum := sha256.Sum256(data)
		ext := "." + format
		if format == "jpeg" {
			ext = ".jpg"
		}
		screens = append(screens, staged{data: data, scr: deliveredScreen{
			Order: i + 1, File: fmt.Sprintf("screens/%02d-%s%s", i+1, slug(strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))), ext),
			Source: filepath.ToSlash(rel), Width: cfg.Width, Height: cfg.Height, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data)),
		}})
	}
	m := designManifest{Name: p.Name, CreatedAt: now().UTC().Format(time.RFC3339), SystemVersion: rec.Version, SystemStatus: status, System: sys, QualityGate: checkDesignSystem(sys), OpenQuestions: p.OpenQuestions}
	for _, s := range screens {
		m.Screens = append(m.Screens, s.scr)
	}
	sources, err := prepareDesignSources(root, p.SourceFiles)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		m.Sources = append(m.Sources, source.info)
		totalBytes += len(source.data)
	}
	if totalBytes > 128<<20 {
		return nil, errors.New("design package exceeds 128 MiB total")
	}
	m.SourceURLs, err = validateDesignURLs(p.SourceURLs)
	if err != nil {
		return nil, err
	}
	m.RequiredScreens = slices.Clone(p.RequiredScreens)
	m.Audits, err = loadPackageAudits(root, p.AuditIDs, m.Screens, m.Sources, now())
	if err != nil {
		return nil, err
	}
	m.Completion = designPackageCompletion(m)
	if _, err := os.Stat(out); err == nil {
		// Delivering the same screens with the same system again returns
		// the existing package instead of a copy; anything else needs a
		// new name, so a delivered package is never overwritten.
		if prev, ok := readManifest(filepath.Join(out, "manifest.json")); ok && sameDelivery(prev, m) {
			return map[string]any{"package": filepath.ToSlash(filepath.Join("design-delivery", p.Name)), "screens": len(prev.Screens), "quality_gate": prev.QualityGate, "unchanged": true, "completion": prev.Completion, "delivery": "assembled_locally_not_presented", "visual_inspection_required": true}, nil
		}
		return nil, fmt.Errorf("design-delivery/%s already exists with different content; choose a new name (packages are not overwritten)", p.Name)
	}
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
