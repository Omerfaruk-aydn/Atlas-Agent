package webverify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDriver scripts a page. Eval results are keyed by which script ran.
type fakeDriver struct {
	navigations []string
	viewports   []string
	navErr      map[string]error
	console     map[string][]ConsoleMessage // by viewport name
	network     map[string][]NetworkEntry
	layout      map[string]string
	a11y        string
	load        string
	asserts     func(Assertion) (bool, string)
	clicks      []string
	current     string
	vp          string
	axeLoaded   bool
	axeInjected int
	pending     []ConsoleMessage
	pendingNet  []NetworkEntry
}

func (d *fakeDriver) Navigate(_ context.Context, url string) error {
	if err := d.navErr[url]; err != nil {
		return err
	}
	d.navigations = append(d.navigations, url)
	d.current = url
	d.pending = d.console[d.vp]
	d.pendingNet = d.network[d.vp]
	return nil
}

func (d *fakeDriver) SetViewport(_ context.Context, v Viewport) error {
	d.vp = v.Name
	d.viewports = append(d.viewports, v.Name)
	return nil
}

func (d *fakeDriver) Eval(_ context.Context, script string, _ time.Duration) (string, error) {
	switch {
	case script == loadTimingScript:
		return d.load, nil
	case script == layoutScript:
		if l, ok := d.layout[d.vp]; ok {
			return l, nil
		}
		return `"{\"inner_width\":390,\"scroll_width\":390,\"scrolls\":false,\"wide\":[],\"blank\":false,\"body\":\"<body>\"}"`, nil
	case script == bodyExcerptScript:
		return `"{\"URL\":\"http://localhost:3000/\",\"Title\":\"App\",\"Text\":\"Welcome\"}"`, nil
	case strings.Contains(script, "typeof window.axe"):
		d.axeInjected++
		return `"\"object\""`, nil
	case script == axeRunScript || script == builtinA11yScript:
		return d.a11y, nil
	}
	return "", errors.New("unexpected script")
}

func (d *fakeDriver) Screenshot(context.Context) (Image, error) {
	return Image{Data: []byte("png-" + d.vp), MediaType: "image/png"}, nil
}

func (d *fakeDriver) ConsoleSince(context.Context) ([]ConsoleMessage, error) {
	out := d.pending
	d.pending = nil
	return out, nil
}

func (d *fakeDriver) NetworkSince(context.Context) ([]NetworkEntry, error) {
	out := d.pendingNet
	d.pendingNet = nil
	return out, nil
}

func (d *fakeDriver) Click(_ context.Context, selector string) error {
	d.clicks = append(d.clicks, selector)
	if selector == "#missing" {
		return errors.New("no element matches #missing")
	}
	return nil
}
func (d *fakeDriver) Type(context.Context, string, string) error { return nil }
func (d *fakeDriver) Press(context.Context, string) error        { return nil }
func (d *fakeDriver) Assert(_ context.Context, a Assertion) (bool, string, error) {
	if d.asserts == nil {
		return true, "", nil
	}
	ok, detail := d.asserts(a)
	return ok, detail, nil
}

func cleanDriver() *fakeDriver {
	return &fakeDriver{a11y: `"[]"`, load: `"{\"load\":300}"`}
}

func TestCheckLocal(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"http://localhost:3000", "https://127.0.0.1:8443/x", "http://[::1]:5173", "http://app.localhost", "http://shop.test/"} {
		_, err := CheckLocal(ok)
		require.NoError(t, err, ok)
	}
	for _, bad := range []string{"https://example.com", "http://localhost.evil.com", "ftp://localhost", "http://user:pw@localhost", "file:///etc/passwd", "http://192.168.1.4", "javascript:alert(1)", "http://0.0.0.0:80"} {
		_, err := CheckLocal(bad)
		require.Error(t, err, bad)
	}
}

func TestResolveRefusesOffSite(t *testing.T) {
	t.Parallel()
	got, err := Resolve("http://localhost:3000", "/about")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/about", got)
	_, err = Resolve("http://localhost:3000", "https://evil.example/")
	require.Error(t, err)
	_, err = Resolve("http://localhost:3000", "//evil.example/")
	require.Error(t, err)
}

func TestDiscoverURL(t *testing.T) {
	t.Parallel()
	require.Equal(t, "http://localhost:5173", DiscoverURL("build a page", "\x1b[32m  Local:   http://localhost:5173/\x1b[0m"))
	require.Equal(t, "http://127.0.0.1:8000", DiscoverURL("serving on 127.0.0.1:8000 now"))
	require.Equal(t, "", DiscoverURL("see https://example.com/docs"))
	// The most recent output wins over the request.
	require.Equal(t, "http://localhost:4000", DiscoverURL("try http://localhost:3000", "ready on http://localhost:4000"))
}

func TestIsWebTask(t *testing.T) {
	t.Parallel()
	require.True(t, IsWebTask("anything", []string{"src/App.tsx"}))
	require.True(t, IsWebTask("fix the login sayfası", []string{"web/app.js"}))
	require.False(t, IsWebTask("speed up the importer", []string{"cmd/import.js"}))
	require.False(t, IsWebTask("fix the login page", []string{"internal/auth/auth.go"}))
}

func TestSeverityAndBlocking(t *testing.T) {
	t.Parallel()
	shot := Evidence{Screenshot: "a.png"}
	require.True(t, Finding{Severity: SeverityMedium, Source: SourceMeasured, Evidence: shot}.Blocks(SeverityMedium))
	require.False(t, Finding{Severity: SeverityLow, Source: SourceMeasured, Evidence: shot}.Blocks(SeverityMedium))
	require.False(t, Finding{Severity: SeverityHigh, Source: SourceMeasured}.Blocks(SeverityLow), "no evidence never blocks")
	require.False(t, Finding{Severity: SeverityHigh, Source: SourceMeasured, Evidence: shot, Advisory: true}.Blocks(SeverityLow))
	require.False(t, Finding{Severity: SeverityMedium, Source: SourceVision, Evidence: shot}.Blocks(SeverityLow), "vision blocks only at high")
	require.True(t, Finding{Severity: SeverityHigh, Source: SourceVision, Evidence: shot}.Blocks(SeverityHigh))
}

func TestRunCleanPage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := cleanDriver()
	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: d, Root: root, RunID: "r1"})
	require.NoError(t, err)
	require.Empty(t, report.Findings)
	require.Equal(t, []string{"mobile", "tablet", "desktop"}, d.viewports)
	require.Len(t, d.navigations, 3)
	require.Equal(t, "built-in subset", report.A11yEngine)
	require.Contains(t, report.Skipped[0], "visual review")
	for _, name := range []string{"home-mobile.png", "home-tablet.png", "home-desktop.png"} {
		require.FileExists(t, filepath.Join(root, ".atlas", "verify", "r1", name))
	}
	require.NoError(t, report.Save(root, SeverityMedium))
	require.FileExists(t, filepath.Join(root, ".atlas", "verify", "r1", "report.md"))
	require.FileExists(t, filepath.Join(root, ".atlas", "verify", "latest.md"))
	require.Empty(t, report.FixPrompt(SeverityMedium, 1, 2))
}

func TestRunRefusesRemoteURL(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), Options{URL: "https://example.com", Driver: cleanDriver()})
	require.Error(t, err)
}

func TestRunFindsTechnicalProblems(t *testing.T) {
	t.Parallel()
	d := cleanDriver()
	d.console = map[string][]ConsoleMessage{
		"mobile": {
			{Type: "error", Text: "TypeError: x is undefined"},
			{Type: "error", Text: "Failed to load resource: the server responded with a status of 404"},
			{Type: "warning", Text: "deprecated"},
		},
	}
	d.network = map[string][]NetworkEntry{
		"mobile": {
			{RequestID: "1", URL: "http://localhost:3000/api/items", Status: 500},
			{RequestID: "2", URL: "http://localhost:3000/favicon.ico", Status: 404},
			{RequestID: "3", URL: "http://localhost:3000/ok.js", Status: 200},
			{RequestID: "4", Failed: true},
		},
	}
	d.layout = map[string]string{
		"mobile": `"{\"inner_width\":390,\"scroll_width\":640,\"scrolls\":true,\"wide\":[{\"target\":\"div.hero\",\"html\":\"<div class=hero>\"}],\"blank\":false,\"body\":\"\"}"`,
	}
	d.a11y = `"[{\"id\":\"image-alt\",\"impact\":\"critical\",\"help\":\"Images must have alternate text\",\"helpUrl\":\"https://x\",\"nodes\":[{\"target\":\"img\",\"html\":\"<img src=a.png>\"}]},{\"id\":\"region\",\"impact\":\"moderate\",\"help\":\"h\",\"nodes\":[{\"target\":\"p\",\"html\":\"<p>\"}]}]"`
	d.load = `"{\"load\":9000}"`

	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: d, Root: t.TempDir(), RunID: "r2"})
	require.NoError(t, err)

	kinds := map[Kind][]Finding{}
	for _, f := range report.Findings {
		kinds[f.Kind] = append(kinds[f.Kind], f)
	}
	require.Len(t, kinds[KindConsole], 1, "the resource-load line belongs to the network check")
	require.Equal(t, "error: TypeError: x is undefined", kinds[KindConsole][0].Evidence.Console)
	require.Len(t, kinds[KindNetwork], 3)
	require.Len(t, kinds[KindLoad], 1, "identical load findings from three viewports merge")
	require.ElementsMatch(t, []string{"mobile", "tablet", "desktop"}, kinds[KindLoad][0].Viewports)
	require.Len(t, kinds[KindLayout], 1)
	require.Equal(t, []string{"mobile"}, kinds[KindLayout][0].Viewports)
	require.NotEmpty(t, kinds[KindLayout][0].Evidence.Screenshot)

	var a11y []Finding
	a11y = append(a11y, kinds[KindA11y]...)
	require.Len(t, a11y, 2)
	require.Equal(t, SeverityHigh, a11y[0].Severity)

	var statuses []Severity
	for _, f := range kinds[KindNetwork] {
		statuses = append(statuses, f.Severity)
	}
	require.ElementsMatch(t, []Severity{SeverityHigh, SeverityLow, SeverityLow}, statuses)

	blocking := report.Blocking(SeverityMedium)
	for _, f := range blocking {
		require.True(t, f.Evidenced())
		require.NotEqual(t, SeverityLow, f.Severity)
	}
	prompt := report.FixPrompt(SeverityMedium, 1, 2)
	require.Contains(t, prompt, "image-alt")
	require.Contains(t, prompt, "/api/items")
	require.Contains(t, prompt, "evidence:")
	require.NotContains(t, prompt, "favicon")
}

func TestBudgetsAllowTolerated(t *testing.T) {
	t.Parallel()
	d := cleanDriver()
	d.console = map[string][]ConsoleMessage{"mobile": {{Type: "error", Text: "one"}}}
	report, err := Run(context.Background(), Options{
		URL: "http://localhost:3000", Driver: d, Budgets: Budgets{ConsoleErrors: 1, LoadMS: 1000}, Viewports: DefaultViewports()[:1],
	})
	require.NoError(t, err)
	require.Empty(t, report.Findings)
}

func TestNavigationFailureIsEvidencedFinding(t *testing.T) {
	t.Parallel()
	d := cleanDriver()
	d.navErr = map[string]error{"http://localhost:3000/": errors.New("net::ERR_CONNECTION_REFUSED")}
	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: d, Viewports: DefaultViewports()[:1]})
	require.NoError(t, err)
	require.Len(t, report.Findings, 1)
	require.Equal(t, KindPage, report.Findings[0].Kind)
	require.True(t, report.Findings[0].Blocks(SeverityMedium))
}

func TestAxeInjection(t *testing.T) {
	t.Parallel()
	d := cleanDriver()
	report, err := Run(context.Background(), Options{
		URL: "http://localhost:3000", Driver: d, Viewports: DefaultViewports()[:2],
		Axe: AxeScript{Source: "window.axe={run:function(){}}", Path: "node_modules/axe-core/axe.min.js"},
	})
	require.NoError(t, err)
	require.Equal(t, "axe-core", report.A11yEngine)
	require.Equal(t, 2, d.axeInjected, "axe must be injected after every page load")
}

func TestFindAxe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.Empty(t, FindAxe(root, "").Source)
	dir := filepath.Join(root, "node_modules", "axe-core")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "axe.min.js"), []byte("/*! axe v4 */ window.axe={}"), 0o644))
	found := FindAxe(root, "")
	require.Contains(t, found.Source, "axe")
	require.Equal(t, "axe-core", found.Name())
	// A file that is not axe is ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "axe.min.js"), []byte("console.log(1)"), 0o644))
	require.Empty(t, FindAxe(root, "").Source)
}

func TestFlows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := cleanDriver()
	d.asserts = func(a Assertion) (bool, string) {
		if a.Expected == "Dashboard" {
			return false, "text_contains failed: body does not contain \"Dashboard\""
		}
		return true, ""
	}
	hard := Flow{Name: "login", Steps: []Step{
		{Action: StepNavigate, URL: "/login"},
		{Action: StepType, Selector: "#email", Text: "a@b.co"},
		{Action: StepClick, Selector: "button[type=submit]"},
		{Action: StepAssert, Condition: "text_contains", Selector: "h1", Expected: "Dashboard"},
	}}
	soft := Flow{Name: "soft", Soft: true, Steps: []Step{{Action: StepAssert, Condition: "text_contains", Selector: "body", Expected: "Dashboard"}}}
	passing := Flow{Name: "home", Steps: []Step{{Action: StepAssert, Condition: "visible", Selector: "body"}}}
	broken := Flow{Name: "broken click", Steps: []Step{{Action: StepClick, Selector: "#missing"}}}

	report, err := Run(context.Background(), Options{
		URL: "http://localhost:3000", Driver: d, Root: root, RunID: "f1", Viewports: DefaultViewports(),
		Flows: []Flow{hard, soft, passing, broken},
	})
	require.NoError(t, err)
	require.Len(t, report.Flows, 4)
	require.False(t, report.Flows[0].Passed)
	require.Equal(t, 4, report.Flows[0].FailedStep)
	require.True(t, report.Flows[2].Passed)
	require.Equal(t, "desktop", d.vp, "flows run at the widest viewport")

	blocking := report.Blocking(SeverityMedium)
	require.Len(t, blocking, 2, "the soft flow is advisory")
	for _, f := range blocking {
		require.Equal(t, KindFlow, f.Kind)
		require.NotEmpty(t, f.Evidence.DOM)
		require.NotEmpty(t, f.Evidence.Screenshot)
		require.FileExists(t, filepath.Join(root, filepath.FromSlash(f.Evidence.Screenshot)))
	}
	md := report.Markdown(SeverityMedium)
	require.Contains(t, md, "advisory")
	require.Contains(t, md, "FAILED at step 4")
}

func TestFlowNavigateOffSiteFails(t *testing.T) {
	t.Parallel()
	d := cleanDriver()
	report, err := Run(context.Background(), Options{
		URL: "http://localhost:3000", Driver: d, Viewports: DefaultViewports()[:1],
		Flows: []Flow{{Name: "escape", Steps: []Step{{Action: StepNavigate, URL: "https://example.com/"}}}},
	})
	require.NoError(t, err)
	require.False(t, report.Flows[0].Passed)
	require.NotContains(t, d.navigations, "https://example.com/")
}

type fakeVision struct {
	reply string
	err   error
	got   []Shot
}

func (v *fakeVision) Review(_ context.Context, shots []Shot) (string, error) {
	v.got = shots
	return v.reply, v.err
}

func TestVisionReview(t *testing.T) {
	t.Parallel()
	vision := &fakeVision{reply: "VERDICT: FAIL\n- [high] mobile | the hero text overlaps the nav\n- [medium] tablet | the footer is cut off\n- [high] watch | made-up screen\nnoise"}
	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: cleanDriver(), Root: t.TempDir(), Vision: vision})
	require.NoError(t, err)
	require.Len(t, vision.got, 3)
	var visual []Finding
	for _, f := range report.Findings {
		if f.Kind == KindVisual {
			visual = append(visual, f)
		}
	}
	require.Len(t, visual, 3)
	blocking := report.Blocking(SeverityLow)
	require.Len(t, blocking, 1, "only the evidenced high vision finding blocks")
	require.Contains(t, blocking[0].Title, "hero text overlaps")
	require.Equal(t, "home-mobile.png", filepath.Base(blocking[0].Evidence.Screenshot))
}

func TestVisionFailureIsReportedNotBlocking(t *testing.T) {
	t.Parallel()
	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: cleanDriver(), Vision: &fakeVision{err: errors.New("no quota")}, Viewports: DefaultViewports()[:1]})
	require.NoError(t, err)
	require.Empty(t, report.Findings)
	require.Contains(t, strings.Join(report.Skipped, "\n"), "visual review failed")
}

func TestVisionPassWithFindingsIsAdvisory(t *testing.T) {
	t.Parallel()
	shots := []Shot{{Viewport: "mobile", Path: "a.png"}}
	findings := ParseVisionReply("VERDICT: PASS\n- [high] mobile | something", shots)
	require.Len(t, findings, 1)
	require.True(t, findings[0].Advisory)
}

func TestParseSpec(t *testing.T) {
	t.Parallel()
	spec, err := ParseSpec([]byte(`{"url":"http://localhost:5173","pages":["/about"],"flows":[{"name":"go","steps":[{"action":"navigate","url":"/"},{"action":"assert","condition":"title","expected":"Home"}]}]}`))
	require.NoError(t, err)
	require.Equal(t, "http://localhost:5173", spec.URL)

	for name, bad := range map[string]string{
		"remote url":  `{"url":"https://example.com"}`,
		"remote page": `{"pages":["https://example.com"]}`,
		"no steps":    `{"flows":[{"name":"x"}]}`,
		"bad action":  `{"flows":[{"name":"x","steps":[{"action":"eval","text":"1"}]}]}`,
		"bad cond":    `{"flows":[{"name":"x","steps":[{"action":"assert","condition":"magic","selector":"a"}]}]}`,
		"no target":   `{"flows":[{"name":"x","steps":[{"action":"assert","condition":"visible"}]}]}`,
		"not json":    `{`,
		"no name":     `{"flows":[{"steps":[{"action":"key","key":"enter"}]}]}`,
	} {
		_, err := ParseSpec([]byte(bad))
		require.Error(t, err, name)
	}
}

func TestLoadSpecMissingIsNil(t *testing.T) {
	t.Parallel()
	spec, err := LoadSpec(t.TempDir())
	require.NoError(t, err)
	require.Nil(t, spec)
}

func TestDeriveFlows(t *testing.T) {
	t.Parallel()
	flows := DeriveFlows("add a /pricing page with a toggle", []Criterion{
		{ID: "F1", Title: "Pricing", Acceptance: `The page shows "Monthly" and "Yearly" plans; the helper my_func() is not shown to users. See the /usr/bin path.`},
	})
	var names []string
	for _, f := range flows {
		require.True(t, f.Soft)
		names = append(names, f.Name)
	}
	require.Contains(t, names, "page /pricing loads")
	require.Contains(t, names, `feature F1 shows "Monthly"`)
	require.Contains(t, names, `feature F1 shows "Yearly"`)
	for _, n := range names {
		require.NotContains(t, n, "my_func")
		require.NotContains(t, n, "/usr")
	}
	merged := MergeFlows([]Flow{{Name: "page /pricing loads"}}, flows)
	require.Len(t, merged, len(flows))
	require.False(t, merged[0].Soft)
}

func TestMarkdownAndSaveRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	d := cleanDriver()
	d.console = map[string][]ConsoleMessage{"mobile": {{Type: "exception", Text: "Uncaught ReferenceError: foo"}}}
	report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: d, Root: root, RunID: "r9", Viewports: DefaultViewports()[:1]})
	require.NoError(t, err)
	require.NoError(t, report.Save(root, SeverityMedium))
	data, err := os.ReadFile(filepath.Join(root, ".atlas", "verify", "r9", "report.json"))
	require.NoError(t, err)
	var back Report
	require.NoError(t, json.Unmarshal(data, &back))
	require.Len(t, back.Findings, 1)
	require.Equal(t, SeverityHigh, back.Findings[0].Severity)
	md, err := os.ReadFile(filepath.Join(root, ".atlas", "verify", "r9", "report.md"))
	require.NoError(t, err)
	require.Contains(t, string(md), "Uncaught ReferenceError")
	require.Contains(t, string(md), "blocking")
}

func TestPruneKeepsNewestRuns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, id := range []string{"20260101-000001", "20260101-000002", "20260101-000003", "20260101-000004", "20260101-000005", "20260101-000006", "20260101-000007"} {
		d := cleanDriver()
		report, err := Run(context.Background(), Options{URL: "http://localhost:3000", Driver: d, Root: root, RunID: id, Viewports: DefaultViewports()[:1]})
		require.NoError(t, err)
		require.NoError(t, report.Save(root, SeverityMedium))
	}
	entries, err := os.ReadDir(filepath.Join(root, ".atlas", "verify"))
	require.NoError(t, err)
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	require.Equal(t, keepRuns, dirs)
	require.NoDirExists(t, filepath.Join(root, ".atlas", "verify", "20260101-000001"))
}

func TestCanceledContextReturnsPartialReport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Run(ctx, Options{URL: "http://localhost:3000", Driver: cleanDriver()})
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, report)
}
