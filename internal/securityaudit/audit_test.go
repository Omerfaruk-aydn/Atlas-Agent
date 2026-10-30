package securityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/depaudit"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gotest"
)

type agentFunc func(ctx context.Context, prompt string) error

func (f agentFunc) Run(ctx context.Context, prompt string) error { return f(ctx, prompt) }

// scenario is a tiny fake world: a vulnerable flag the fix pass flips, and
// switches for the ways a fix can go wrong.
type scenario struct {
	fixed     atomic.Bool
	regressed atomic.Bool
}

func (s *scenario) runner() TestRunner {
	return func(_ context.Context, _ string, opts gotest.Options) (gotest.Result, error) {
		if opts.Packages == "./..." {
			res := gotest.Result{
				Tests:    []gotest.Test{{Package: "example.com/app", Name: "TestOther", Status: gotest.StatusPass}},
				Packages: []gotest.PackageResult{{Package: "example.com/app"}},
			}
			if !s.fixed.Load() {
				res.Tests = append(res.Tests, gotest.Test{Package: "example.com/app", Name: "TestSecurityProof_V1", Status: gotest.StatusFail})
			}
			if s.regressed.Load() {
				res.Tests = append(res.Tests, gotest.Test{Package: "example.com/app", Name: "TestBroken", Status: gotest.StatusFail})
			}
			return res, nil
		}
		if s.fixed.Load() {
			return testResult("TestSecurityProof_V1", gotest.StatusPass, ""), nil
		}
		return testResult("TestSecurityProof_V1", gotest.StatusFail, "app_test.go:9: [V1] traversal escaped base"), nil
	}
}

const manifestJSON = `{"version":1,"findings":[
 {"id":"V1","title":"Path traversal in download","cwe":"CWE-22","severity":"high","file":"app/serve.go","line":10,
  "status":"confirmed","proof":{"file":"app/securityproof_V1_test.go","test":"TestSecurityProof_V1"}},
 {"id":"V2","title":"Missing rate limit","cwe":"CWE-770","severity":"medium","file":"app/serve.go","line":30,
  "description":"no limiter"}
]}`

// discoveryAgent writes the manifest and proof like a model would, and
// optionally reacts to the fix prompt.
func discoveryAgent(t *testing.T, root string, onFix func()) agentFunc {
	t.Helper()
	return func(_ context.Context, prompt string) error {
		if strings.Contains(prompt, "Run a defensive security audit") {
			writeFile(t, filepath.Join(root, "app", "securityproof_V1_test.go"), proofSource)
			writeFile(t, filepath.Join(root, ".atlas", "security", ManifestFile), manifestJSON)
			return nil
		}
		if strings.Contains(prompt, "Fix the confirmed security findings") && onFix != nil {
			onFix()
		}
		return nil
	}
}

func TestAuditConfirmsOnlyProvenFindings(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	sc := &scenario{}
	report, err := Audit(t.Context(), Options{
		Root: root, Run: sc.runner(),
		Agent: discoveryAgent(t, root, nil),
	})
	require.NoError(t, err)

	byID := map[string]Finding{}
	for _, f := range report.Findings {
		byID[f.ID] = f
	}
	require.Equal(t, StatusConfirmed, byID["V1"].Status, byID["V1"].Reason)
	require.Equal(t, StatusUnconfirmed, byID["V2"].Status, "no proof means unconfirmed")
	require.Contains(t, byID["V2"].Reason, "no proof")
	require.Equal(t, "V1", report.Findings[0].ID, "confirmed findings are ranked first")

	data, err := os.ReadFile(filepath.Join(root, ".atlas", "security", "report.json"))
	require.NoError(t, err)
	var saved Report
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Len(t, saved.Findings, len(report.Findings))
	_, err = os.Stat(filepath.Join(root, ".atlas", "security", "report.md"))
	require.NoError(t, err)
}

func TestAuditWithoutAgentReportsLeadsAsUnconfirmed(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	writeFile(t, filepath.Join(root, "app", "creds.go"), `package app

const apiKey = "sk_live_9f8a7b6c5d4e3f2a1b0c"
`)
	report, err := Audit(t.Context(), Options{
		Root: root,
		Deps: func(context.Context, string) (depaudit.Result, error) {
			return depaudit.Result{VulnToolAvailable: true, Vulnerabilities: []depaudit.Vulnerability{
				{ID: "GO-2024-0001", Summary: "bad parser", Module: "example.com/dep", Found: "v1.0.0", Fixed: "v1.0.1", Called: true},
				{ID: "GO-2024-0002", Summary: "other", Module: "example.com/dep2", Found: "v1.0.0"},
			}}, nil
		},
	})
	require.NoError(t, err)
	require.Zero(t, report.Count(StatusConfirmed))
	require.NotEmpty(t, report.Findings)
	var cwes []string
	for _, f := range report.Findings {
		require.Equal(t, StatusUnconfirmed, f.Status)
		cwes = append(cwes, f.CWE)
	}
	require.Contains(t, cwes, "CWE-798")
	require.Contains(t, cwes, "CWE-1395")
	require.Equal(t, SeverityHigh, report.Findings[0].Severity)
	require.NotEmpty(t, report.Notes)
}

func TestAuditDeduplicatesStaticCandidateCoveredByAgentFinding(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	writeFile(t, filepath.Join(root, "app", "creds.go"), "package app\n\nconst apiKey = \"sk_live_9f8a7b6c5d4e3f2a1b0c\"\n")
	writeFile(t, filepath.Join(root, ".atlas", "security", ManifestFile), `{"version":1,"findings":[{"id":"V9","title":"Leaked key","cwe":"CWE-798","severity":"high","file":"app/creds.go","line":3}]}`)
	report, err := Audit(t.Context(), Options{Root: root})
	require.NoError(t, err)
	n := 0
	for _, f := range report.Findings {
		if f.CWE == "CWE-798" {
			n++
		}
	}
	require.Equal(t, 1, n)
}

func TestAuditAgentFailureIsANoteNotAnError(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	report, err := Audit(t.Context(), Options{Root: root, Agent: agentFunc(func(context.Context, string) error {
		return errors.New("model unavailable")
	})})
	require.NoError(t, err)
	require.Contains(t, strings.Join(report.Notes, "\n"), "model unavailable")
}

func TestAuditRejectsBadScopeAndRoot(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	_, err := Audit(t.Context(), Options{Root: root, Scope: ".."})
	require.ErrorContains(t, err, "outside the project")
	_, err = Audit(t.Context(), Options{Root: filepath.Join(root, "missing")})
	require.ErrorContains(t, err, "not a directory")
	_, err = Audit(t.Context(), Options{Root: root, Scope: "nope"})
	require.ErrorContains(t, err, "not a directory")
}

func TestAuditCancelledContext(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Audit(ctx, Options{Root: root})
	require.ErrorIs(t, err, context.Canceled)
}

func fixed(t *testing.T, mutate func(sc *scenario, root string)) Report {
	t.Helper()
	root := newRoot(t)
	sc := &scenario{}
	report, err := Audit(t.Context(), Options{
		Root: root, Run: sc.runner(), Fix: true,
		Agent: discoveryAgent(t, root, func() { mutate(sc, root) }),
	})
	require.NoError(t, err)
	return report
}

func findingByID(t *testing.T, r Report, id string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("finding %s missing", id)
	return Finding{}
}

func TestFixMarksFixedWhenProofGreenAndSuiteStable(t *testing.T) {
	t.Parallel()
	report := fixed(t, func(sc *scenario, _ string) { sc.fixed.Store(true) })
	f := findingByID(t, report, "V1")
	require.Equal(t, StatusFixed, f.Status, f.Reason)
	require.Empty(t, report.Regressions)
}

func TestFixFailsWhenProofStillRed(t *testing.T) {
	t.Parallel()
	report := fixed(t, func(*scenario, string) {})
	f := findingByID(t, report, "V1")
	require.Equal(t, StatusFixFailed, f.Status)
	require.Contains(t, f.Reason, "still fails")
	require.NotEmpty(t, report.Blocking(SeverityHigh))
}

func TestFixFailsWhenProofWasEdited(t *testing.T) {
	t.Parallel()
	report := fixed(t, func(sc *scenario, root string) {
		sc.fixed.Store(true)
		writeFile(t, filepath.Join(root, "app", "securityproof_V1_test.go"), strings.Replace(proofSource, "got != 2", "got != 1", 1))
	})
	f := findingByID(t, report, "V1")
	require.Equal(t, StatusFixFailed, f.Status)
	require.Contains(t, f.Reason, "modified")
}

func TestFixFailsWhenOtherTestsRegress(t *testing.T) {
	t.Parallel()
	report := fixed(t, func(sc *scenario, _ string) {
		sc.fixed.Store(true)
		sc.regressed.Store(true)
	})
	f := findingByID(t, report, "V1")
	require.Equal(t, StatusFixFailed, f.Status)
	require.Contains(t, f.Reason, "TestBroken")
	require.Equal(t, []string{"example.com/app.TestBroken"}, report.Regressions)
}

func TestFixWithoutConfirmedFindingsChangesNothing(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	calls := 0
	report, err := Audit(t.Context(), Options{Root: root, Fix: true, Agent: agentFunc(func(context.Context, string) error {
		calls++
		return nil
	})})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "only the discovery pass runs")
	require.Contains(t, strings.Join(report.Notes, "\n"), "nothing was changed")
}

func TestBuildThreatModelFindsEntryPointsAndSinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), `package main

import (
	"net/http"
	"os/exec"
	"os"
)

func main() {
	http.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		exec.Command("sh", "-c", r.URL.Query().Get("c")).Run()
	})
	_ = os.Getenv("TOKEN")
}
`)
	writeFile(t, filepath.Join(root, "main_test.go"), "package main\n\nimport \"os/exec\"\n\nvar _ = exec.Command\n")
	writeFile(t, filepath.Join(root, "node_modules", "x", "a.js"), "console.log(1)")
	writeFile(t, filepath.Join(root, "ui", "app.ts"), "export {}")

	m, err := BuildThreatModel(t.Context(), root, "")
	require.NoError(t, err)
	require.Equal(t, map[string]int{"go": 2, "typescript": 1}, m.Languages, "node_modules is skipped")
	kinds := func(sigs []Signal) []string {
		var out []string
		for _, s := range sigs {
			out = append(out, s.Kind)
		}
		return out
	}
	require.Contains(t, kinds(m.EntryPoints), "http-handler")
	require.Contains(t, kinds(m.EntryPoints), "environment")
	require.Equal(t, []string{"command-exec"}, kinds(m.Sinks), "test files are not part of the attack surface")
	require.Equal(t, []string{"main.go"}, m.Hotspots)
	require.NotEmpty(t, m.Boundaries)
}

func TestBuildThreatModelCapsSignals(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("package a\n\nimport \"os\"\n\nfunc f() {\n")
	for range 50 {
		b.WriteString("\t_ = os.Getenv(\"X\")\n")
	}
	b.WriteString("}\n")
	writeFile(t, filepath.Join(root, "a.go"), b.String())
	m, err := BuildThreatModel(t.Context(), root, "")
	require.NoError(t, err)
	require.Len(t, m.EntryPoints, maxSignalsPerKind)
	require.True(t, m.Truncated)
}

func TestRenderMarkdownSeparatesConfirmedFromUnconfirmed(t *testing.T) {
	t.Parallel()
	r := Report{Findings: []Finding{
		{ID: "A", Title: "proven", CWE: "CWE-22", Severity: SeverityHigh, Status: StatusConfirmed, Evidence: "[A] bad", Proof: &Proof{File: "a_test.go", Test: "TestA"}},
		{ID: "B", Title: "guess", CWE: "CWE-89", Severity: SeverityCritical, Status: StatusUnconfirmed, Reason: "no proof"},
	}, Notes: []string{"govulncheck missing"}}
	md := RenderMarkdown(r)
	confirmed := strings.Index(md, "## Confirmed, open (1)")
	unconfirmed := strings.Index(md, "## Unconfirmed (NOT demonstrated) (1)")
	require.Positive(t, confirmed)
	require.Greater(t, unconfirmed, confirmed)
	require.Greater(t, strings.Index(md, "### [B]"), unconfirmed)
	require.Less(t, strings.Index(md, "### [A]"), unconfirmed)
	require.Contains(t, md, "CWE-22")
	require.Contains(t, md, "govulncheck missing")
	_, err := RenderJSON(r)
	require.NoError(t, err)
}

func TestBlockingHonoursThreshold(t *testing.T) {
	t.Parallel()
	r := Report{Findings: []Finding{
		{ID: "A", Severity: SeverityLow, Status: StatusConfirmed},
		{ID: "B", Severity: SeverityCritical, Status: StatusUnconfirmed},
		{ID: "C", Severity: SeverityHigh, Status: StatusFixed},
		{ID: "D", Severity: SeverityHigh, Status: StatusFixFailed},
	}}
	require.Len(t, r.Blocking(SeverityLow), 2)
	require.Len(t, r.Blocking(SeverityHigh), 1)
}
