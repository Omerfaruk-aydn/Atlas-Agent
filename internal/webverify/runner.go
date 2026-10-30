package webverify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Options configure one verification run.
type Options struct {
	// URL is the address of the running product; it must be local.
	URL string
	// Pages are page paths measured in addition to "/".
	Pages []string
	// Viewports default to DefaultViewports.
	Viewports []Viewport
	// Flows are driven at the widest viewport after the page checks.
	Flows []Flow
	// Budgets default to DefaultBudgets.
	Budgets Budgets
	// Axe is the accessibility engine; the zero value uses the built-in
	// subset.
	Axe    AxeScript
	Driver Driver
	// Vision reviews the screenshots for layout defects. Optional.
	Vision VisionReviewer
	// Root is the project root; evidence is written under it.
	Root string
	// RunID names the evidence directory; it defaults to a timestamp.
	RunID string
	Now   func() time.Time
}

// FlowResult is the outcome of one flow.
type FlowResult struct {
	Name       string `json:"name"`
	Source     string `json:"source,omitempty"`
	Soft       bool   `json:"soft,omitempty"`
	Passed     bool   `json:"passed"`
	FailedStep int    `json:"failed_step,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Report is the result of a run.
type Report struct {
	RunID      string       `json:"run_id"`
	URL        string       `json:"url"`
	StartedAt  time.Time    `json:"started_at"`
	DurationMS int64        `json:"duration_ms"`
	Pages      []string     `json:"pages"`
	Viewports  []string     `json:"viewports"`
	A11yEngine string       `json:"a11y_engine"`
	Flows      []FlowResult `json:"flows,omitempty"`
	Findings   []Finding    `json:"findings"`
	// Skipped lists checks that could not run, with the reason. A skipped
	// check is not a pass.
	Skipped []string `json:"skipped,omitempty"`
	// Dir is the project-relative evidence directory.
	Dir string `json:"dir,omitempty"`
}

const (
	maxEvalTimeout = 10 * time.Second
	axeTimeout     = 30 * time.Second
	maxShots       = 6
	maxPerKind     = 10
)

type runState struct {
	o        Options
	base     string
	r        *Report
	findings []Finding
	shots    []Shot
	axeOK    bool
	axeNoted bool
	// fatal is set when the browser cannot be used at all, such as a
	// denied permission; the run stops and reports it.
	fatal error
}

// Run verifies the product and returns the report. A canceled context
// returns the partial report with the context error.
func Run(ctx context.Context, o Options) (*Report, error) {
	if o.Driver == nil {
		return nil, errors.New("no browser driver")
	}
	if _, err := CheckLocal(o.URL); err != nil {
		return nil, err
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if len(o.Viewports) == 0 {
		o.Viewports = DefaultViewports()
	}
	if o.Budgets.LoadMS == 0 {
		o.Budgets.LoadMS = DefaultBudgets().LoadMS
	}
	started := o.Now()
	if o.RunID == "" {
		o.RunID = started.UTC().Format("20060102-150405")
	}
	s := &runState{o: o, base: o.URL}
	s.r = &Report{RunID: o.RunID, URL: o.URL, StartedAt: started, A11yEngine: o.Axe.Name()}
	if o.Root != "" {
		s.r.Dir = path.Join(".atlas", "verify", o.RunID)
	}
	pages := []string{"/"}
	for _, p := range o.Pages {
		if p = strings.TrimSpace(p); p != "" && !slices.Contains(pages, p) && len(pages) < maxPages {
			pages = append(pages, p)
		}
	}
	s.r.Pages = pages
	for _, v := range o.Viewports {
		s.r.Viewports = append(s.r.Viewports, v.Name)
	}

	err := s.run(ctx, pages)
	s.r.Findings = mergeFindings(s.findings)
	s.r.DurationMS = o.Now().Sub(started).Milliseconds()
	return s.r, err
}

func (s *runState) run(ctx context.Context, pages []string) error {
	for _, page := range pages {
		target, err := Resolve(s.base, page)
		if err != nil {
			s.skipf("page %s: %v", page, err)
			continue
		}
		for _, vp := range s.o.Viewports {
			if err := ctx.Err(); err != nil {
				return err
			}
			s.checkPage(ctx, page, target, vp)
			if s.fatal != nil {
				return s.fatal
			}
		}
	}
	s.visualReview(ctx)
	return s.runFlows(ctx)
}

func (s *runState) skipf(format string, args ...any) {
	s.r.Skipped = append(s.r.Skipped, fmt.Sprintf(format, args...))
}

func (s *runState) add(f Finding) {
	if f.Source == "" {
		f.Source = SourceMeasured
	}
	f.Title = clip(f.Title, 200)
	f.Detail = clip(f.Detail, 600)
	f.Evidence.DOM = clip(f.Evidence.DOM, 700)
	f.Evidence.Console = clip(f.Evidence.Console, 400)
	f.Evidence.Error = clip(f.Evidence.Error, 400)
	f.ID = f.key()
	s.findings = append(s.findings, f)
}

func (s *runState) checkPage(ctx context.Context, page, target string, vp Viewport) {
	drv := s.o.Driver
	if err := drv.SetViewport(ctx, vp); err != nil {
		if errors.Is(err, ErrUnavailable) {
			s.fatal = err
			return
		}
		s.skipf("viewport %s on %s: %v", vp.Name, page, err)
		return
	}
	// Anything recorded before this load belongs to earlier work.
	_, _ = drv.ConsoleSince(ctx)
	_, _ = drv.NetworkSince(ctx)

	start := s.o.Now()
	if err := drv.Navigate(ctx, target); err != nil {
		if errors.Is(err, ErrUnavailable) {
			s.fatal = err
			return
		}
		s.add(Finding{
			Kind: KindPage, Severity: SeverityHigh, Page: page, Viewports: []string{vp.Name},
			Title:    "Page failed to load",
			Detail:   "Navigating to " + target + " did not complete.",
			Evidence: Evidence{Error: err.Error()},
		})
		return
	}
	wall := s.o.Now().Sub(start)
	shot := s.capture(ctx, page, vp, "")

	s.checkLoad(ctx, page, vp, wall)
	s.checkLayout(ctx, page, vp, shot)
	s.checkA11y(ctx, page, vp)
	s.checkRuntime(ctx, page, vp.Name)
}

// capture takes a screenshot and stores it. It returns the project-relative
// path, empty when the image could not be kept.
func (s *runState) capture(ctx context.Context, page string, vp Viewport, suffix string) string {
	img, err := s.o.Driver.Screenshot(ctx)
	if err != nil || len(img.Data) == 0 {
		if err != nil {
			s.skipf("screenshot %s on %s: %v", vp.Name, page, err)
		}
		return ""
	}
	name := fmt.Sprintf("%s-%s%s%s", slug(page), vp.Name, suffix, img.Extension())
	rel := s.store(name, img.Data)
	if len(s.shots) < maxShots && suffix == "" {
		s.shots = append(s.shots, Shot{Viewport: vp.Name, Page: page, Path: rel, Data: img.Data, MediaType: img.MediaType})
	}
	return rel
}

// store writes evidence and returns its project-relative path.
func (s *runState) store(name string, data []byte) string {
	if s.o.Root == "" || s.r.Dir == "" {
		return ""
	}
	dir := filepath.Join(s.o.Root, filepath.FromSlash(s.r.Dir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.skipf("evidence directory: %v", err)
		return ""
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		s.skipf("evidence file %s: %v", name, err)
		return ""
	}
	return path.Join(s.r.Dir, name)
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(page string) string {
	s := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(page), "-"), "-")
	if s == "" {
		return "home"
	}
	return clip(s, 40)
}

type loadTiming struct {
	TTFB             int `json:"ttfb"`
	DOMContentLoaded int `json:"dom_content_loaded"`
	Load             int `json:"load"`
	TransferBytes    int `json:"transfer_bytes"`
}

func (s *runState) checkLoad(ctx context.Context, page string, vp Viewport, wall time.Duration) {
	if s.o.Budgets.LoadMS <= 0 {
		return
	}
	var timing *loadTiming
	if raw, err := s.o.Driver.Eval(ctx, loadTimingScript, maxEvalTimeout); err == nil {
		_ = decodeJSONString(raw, &timing)
	}
	loadMS := int(wall.Milliseconds())
	if timing != nil && timing.Load > 0 {
		loadMS = timing.Load
	}
	if loadMS <= s.o.Budgets.LoadMS {
		return
	}
	measure, _ := json.Marshal(map[string]any{
		"load_ms": loadMS, "budget_ms": s.o.Budgets.LoadMS, "navigation_wall_ms": wall.Milliseconds(), "timing": timing,
	})
	s.add(Finding{
		Kind: KindLoad, Severity: SeverityMedium, Page: page, Viewports: []string{vp.Name},
		Title:    fmt.Sprintf("Page load exceeds the %d ms budget", s.o.Budgets.LoadMS),
		Detail:   fmt.Sprintf("%s took %d ms to load.", page, loadMS),
		Evidence: Evidence{Measure: string(measure)},
	})
}

type layoutResult struct {
	InnerWidth  int        `json:"inner_width"`
	ScrollWidth int        `json:"scroll_width"`
	Scrolls     bool       `json:"scrolls"`
	Wide        []A11yNode `json:"wide"`
	Blank       bool       `json:"blank"`
	Body        string     `json:"body"`
}

func (s *runState) checkLayout(ctx context.Context, page string, vp Viewport, shot string) {
	raw, err := s.o.Driver.Eval(ctx, layoutScript, maxEvalTimeout)
	if err != nil {
		s.skipf("layout measurement %s on %s: %v", vp.Name, page, err)
		return
	}
	var res layoutResult
	if err := decodeJSONString(raw, &res); err != nil {
		s.skipf("layout measurement %s on %s: %v", vp.Name, page, err)
		return
	}
	if res.Blank {
		s.add(Finding{
			Kind: KindLayout, Severity: SeverityHigh, Page: page, Viewports: []string{vp.Name},
			Title:    "Page renders blank",
			Detail:   "The page has no visible text, images, canvas or media.",
			Evidence: Evidence{Screenshot: shot, DOM: res.Body},
		})
	}
	if res.Scrolls {
		var dom []string
		for _, n := range res.Wide {
			dom = append(dom, n.Target+": "+n.HTML)
		}
		if len(dom) == 0 {
			dom = append(dom, res.Body)
		}
		measure, _ := json.Marshal(map[string]int{"inner_width": res.InnerWidth, "scroll_width": res.ScrollWidth})
		s.add(Finding{
			Kind: KindLayout, Severity: SeverityMedium, Page: page, Viewports: []string{vp.Name},
			Title:    "Page scrolls horizontally",
			Detail:   fmt.Sprintf("Content is %d px wide in a %d px viewport, so users must scroll sideways.", res.ScrollWidth, res.InnerWidth),
			Evidence: Evidence{Screenshot: shot, DOM: strings.Join(dom, "\n"), Measure: string(measure)},
		})
	}
}

func (s *runState) checkA11y(ctx context.Context, page string, vp Viewport) {
	drv := s.o.Driver
	script := builtinA11yScript
	if s.o.Axe.Source != "" {
		if raw, err := drv.Eval(ctx, s.o.Axe.injectScript(), axeTimeout); err == nil && strings.Contains(raw, "object") {
			script = axeRunScript
			s.axeOK = true
		} else if !s.axeNoted {
			s.axeNoted = true
			s.skipf("axe-core at %s did not load (%s); the built-in accessibility subset ran instead", s.o.Axe.Path, errText(err, raw))
			s.r.A11yEngine = "built-in subset"
		}
	}
	raw, err := drv.Eval(ctx, script, axeTimeout)
	if err != nil {
		s.skipf("accessibility audit %s on %s: %v", vp.Name, page, err)
		return
	}
	violations, err := ParseA11y(raw)
	if err != nil {
		s.skipf("accessibility audit %s on %s: %v", vp.Name, page, err)
		return
	}
	for _, v := range violations {
		s.add(a11yFinding(v, page, vp.Name))
	}
}

func errText(err error, raw string) string {
	if err != nil {
		return err.Error()
	}
	return "unexpected result " + clip(raw, 60)
}

func a11yFinding(v A11yViolation, page, viewport string) Finding {
	severity := SeverityLow
	switch strings.ToLower(v.Impact) {
	case "critical":
		severity = SeverityHigh
	case "serious":
		severity = SeverityMedium
	}
	var dom []string
	for _, n := range v.Nodes {
		dom = append(dom, n.Target+": "+n.HTML)
	}
	detail := v.Help
	if v.HelpURL != "" {
		detail += " (" + v.HelpURL + ")"
	}
	return Finding{
		Kind: KindA11y, Severity: severity, Page: page, Viewports: []string{viewport},
		Title:    fmt.Sprintf("Accessibility: %s (%s)", v.ID, v.Impact),
		Detail:   detail,
		Evidence: Evidence{DOM: strings.Join(dom, "\n")},
	}
}

// checkRuntime reads what the console and the network recorded since the
// page started loading.
func (s *runState) checkRuntime(ctx context.Context, page, viewport string) {
	drv := s.o.Driver
	messages, err := drv.ConsoleSince(ctx)
	if err != nil {
		s.skipf("console on %s: %v", page, err)
	}
	var errs []ConsoleMessage
	for _, m := range messages {
		t := strings.ToLower(m.Type)
		if (t == "error" || t == "exception") && !strings.HasPrefix(m.Text, "Failed to load resource") {
			errs = append(errs, m)
		}
	}
	if len(errs) > s.o.Budgets.ConsoleErrors {
		for _, m := range errs[:min(len(errs), maxPerKind)] {
			sev := SeverityMedium
			if strings.EqualFold(m.Type, "exception") {
				sev = SeverityHigh
			}
			s.add(Finding{
				Kind: KindConsole, Severity: sev, Page: page, Viewports: []string{viewport},
				Title:    "Console " + strings.ToLower(m.Type) + ": " + clip(firstLine(m.Text), 120),
				Detail:   fmt.Sprintf("%d console error(s) against a budget of %d.", len(errs), s.o.Budgets.ConsoleErrors),
				Evidence: Evidence{Console: m.Type + ": " + m.Text},
			})
		}
	}

	entries, err := drv.NetworkSince(ctx)
	if err != nil {
		s.skipf("network on %s: %v", page, err)
	}
	var bad []NetworkEntry
	for _, e := range entries {
		if e.Failed || e.Status >= 400 {
			bad = append(bad, e)
		}
	}
	if len(bad) > s.o.Budgets.FailedRequests {
		for _, e := range bad[:min(len(bad), maxPerKind)] {
			e := e
			s.add(networkFinding(e, page, viewport, len(bad), s.o.Budgets.FailedRequests))
		}
	}
}

func networkFinding(e NetworkEntry, page, viewport string, count, budget int) Finding {
	sev, title := SeverityMedium, ""
	switch {
	case e.Failed && e.URL == "":
		// An aborted request carries no address: navigation and
		// prefetch cancellations look the same, so it cannot block.
		sev, title = SeverityLow, "A request failed before a response (no address recorded)"
	case e.Failed:
		title = "Request failed: " + e.URL
	case e.Status >= 500:
		sev, title = SeverityHigh, fmt.Sprintf("Request returned %d: %s", e.Status, e.URL)
	default:
		title = fmt.Sprintf("Request returned %d: %s", e.Status, e.URL)
		if strings.HasSuffix(strings.SplitN(e.URL, "?", 2)[0], "/favicon.ico") {
			sev = SeverityLow
		}
	}
	return Finding{
		Kind: KindNetwork, Severity: sev, Page: page, Viewports: []string{viewport},
		Title:    title,
		Detail:   fmt.Sprintf("%d failed request(s) against a budget of %d.", count, budget),
		Evidence: Evidence{Network: &e},
	}
}

// visualReview sends the kept screenshots to the vision reviewer.
func (s *runState) visualReview(ctx context.Context) {
	if len(s.shots) == 0 {
		return
	}
	if s.o.Vision == nil {
		s.skipf("visual review: no vision-capable model is configured")
		return
	}
	reply, err := s.o.Vision.Review(ctx, s.shots)
	if err != nil {
		s.skipf("visual review failed: %v", err)
		return
	}
	for _, f := range ParseVisionReply(reply, s.shots) {
		s.add(f)
	}
}

func (s *runState) runFlows(ctx context.Context) error {
	if len(s.o.Flows) == 0 {
		return nil
	}
	vp := s.o.Viewports[0]
	for _, v := range s.o.Viewports {
		if v.Width > vp.Width {
			vp = v
		}
	}
	if err := s.o.Driver.SetViewport(ctx, vp); err != nil {
		s.skipf("flows: viewport %s: %v", vp.Name, err)
		return nil
	}
	for i, flow := range s.o.Flows {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.r.Flows = append(s.r.Flows, s.runFlow(ctx, i, flow, vp))
	}
	return nil
}

func (s *runState) runFlow(ctx context.Context, index int, flow Flow, vp Viewport) FlowResult {
	drv := s.o.Driver
	res := FlowResult{Name: flow.Name, Source: flow.Source, Soft: flow.Soft}
	if len(flow.Steps) == 0 {
		res.Detail = "the flow has no steps"
		return res
	}
	_, _ = drv.ConsoleSince(ctx)
	_, _ = drv.NetworkSince(ctx)

	fail := func(step int, detail string) FlowResult {
		res.FailedStep, res.Detail = step, detail
		shot := ""
		if img, err := drv.Screenshot(ctx); err == nil && len(img.Data) > 0 {
			shot = s.store(fmt.Sprintf("flow-%d-step-%d%s", index+1, step, img.Extension()), img.Data)
		}
		var excerpt string
		if raw, err := drv.Eval(ctx, bodyExcerptScript, maxEvalTimeout); err == nil {
			var page struct{ URL, Title, Text string }
			if decodeJSONString(raw, &page) == nil {
				excerpt = fmt.Sprintf("%s (%s): %s", page.URL, page.Title, page.Text)
			}
		}
		severity := SeverityHigh
		if flow.Soft {
			severity = SeverityMedium
		}
		s.add(Finding{
			Kind: KindFlow, Severity: severity, Advisory: flow.Soft, Page: "flow:" + flow.Name,
			Viewports: []string{vp.Name},
			Title:     fmt.Sprintf("Flow %q failed at step %d (%s)", flow.Name, step, describeStep(flow.Steps[step-1])),
			Detail:    detail,
			Evidence:  Evidence{Screenshot: shot, DOM: excerpt, Error: detail},
		})
		return res
	}

	if len(flow.Steps) > 0 && flow.Steps[0].Action != StepNavigate {
		if err := drv.Navigate(ctx, s.base); err != nil {
			return fail(1, "could not open the start page: "+err.Error())
		}
	}
	for i, step := range flow.Steps {
		if err := ctx.Err(); err != nil {
			res.Detail = err.Error()
			return res
		}
		if detail := s.runStep(ctx, step); detail != "" {
			return fail(i+1, detail)
		}
	}
	res.Passed = true
	// Errors the flow provoked are findings too.
	s.checkRuntime(ctx, "flow:"+flow.Name, vp.Name)
	return res
}

// runStep returns an empty string on success, else why the step failed.
func (s *runState) runStep(ctx context.Context, step Step) string {
	drv := s.o.Driver
	switch step.Action {
	case StepNavigate:
		target, err := Resolve(s.base, step.URL)
		if err != nil {
			return err.Error()
		}
		if err := drv.Navigate(ctx, target); err != nil {
			return err.Error()
		}
	case StepClick:
		if err := drv.Click(ctx, step.Selector); err != nil {
			return err.Error()
		}
	case StepType:
		if err := drv.Type(ctx, step.Selector, step.Text); err != nil {
			return err.Error()
		}
	case StepKey:
		if err := drv.Press(ctx, step.Key); err != nil {
			return err.Error()
		}
	case StepAssert:
		ok, detail, err := drv.Assert(ctx, Assertion{
			Condition: step.Condition, Selector: step.Selector, Role: step.Role, Name: step.Name,
			Label: step.Label, Expected: step.Expected, TimeoutMS: step.TimeoutMS,
		})
		if err != nil {
			return err.Error()
		}
		if !ok {
			return firstNonEmpty(detail, "the assertion did not hold")
		}
	default:
		return "unknown step action " + step.Action
	}
	return ""
}

func describeStep(st Step) string {
	target := firstNonEmpty(st.Selector, strings.TrimSpace(st.Role+" "+st.Name+" "+st.Label))
	switch st.Action {
	case StepNavigate:
		return "navigate " + st.URL
	case StepClick:
		return "click " + target
	case StepType:
		return "type into " + target
	case StepKey:
		return "press " + st.Key
	case StepAssert:
		return strings.TrimSpace("assert " + st.Condition + " " + target + " " + clip(st.Expected, 40))
	}
	return st.Action
}

// mergeFindings joins identical findings seen at several viewports and
// orders the result by severity.
func mergeFindings(in []Finding) []Finding {
	var out []Finding
	index := map[string]int{}
	for _, f := range in {
		if at, ok := index[f.ID]; ok {
			for _, v := range f.Viewports {
				if !slices.Contains(out[at].Viewports, v) {
					out[at].Viewports = append(out[at].Viewports, v)
				}
			}
			if out[at].Evidence.Screenshot == "" {
				out[at].Evidence.Screenshot = f.Evidence.Screenshot
			}
			continue
		}
		index[f.ID] = len(out)
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b Finding) int { return b.Severity.rank() - a.Severity.rank() })
	return out
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
