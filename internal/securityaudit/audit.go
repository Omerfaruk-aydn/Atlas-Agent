package securityaudit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/gotest"
)

// AgentRunner runs one agent turn with the given instruction. The command
// layer implements it by spawning `atlas-agent run --role security`; tests
// use a fake that writes proof files.
type AgentRunner interface {
	Run(ctx context.Context, prompt string) error
}

// Options configure an audit.
type Options struct {
	// Root is the project root. Required.
	Root string
	// Scope narrows discovery to a path under Root. Verification of
	// proofs and the regression suite still cover the whole module.
	Scope string
	// StateDir holds the manifest and reports. Empty means
	// Root/.atlas/security.
	StateDir string
	// Agent runs discovery (and fixing). Nil means no agent: only the
	// existing manifest, static candidates and dependency advisories are
	// reported.
	Agent AgentRunner
	// Fix asks the agent to fix confirmed findings and then re-verifies.
	Fix bool
	// Deps scans dependencies. Nil skips the dependency check.
	Deps DependencyScanner
	// Run executes tests. Nil means gotest.Run.
	Run TestRunner
	// Repeat is how many failing runs confirm a proof. Zero means two.
	Repeat int
	// ProofTimeout bounds each proof run. Zero means one minute.
	ProofTimeout time.Duration
	// SuiteTimeout bounds each full-suite run used for regression
	// checks. Zero means ten minutes.
	SuiteTimeout time.Duration
}

func (o Options) verifier() Verifier {
	return Verifier{Root: o.Root, Run: o.Run, Repeat: o.Repeat, Timeout: o.ProofTimeout}
}

func (o Options) runner() TestRunner {
	if o.Run != nil {
		return o.Run
	}
	return gotest.Run
}

// Audit builds the threat model, collects findings, verifies proofs and,
// when asked, fixes what was confirmed. Agent and tool failures are
// recorded as notes rather than aborting: a report that says "the agent
// failed, here is what static analysis found" beats no report. Only a
// cancelled context or an unusable root is returned as an error.
func Audit(ctx context.Context, opts Options) (Report, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Report{}, err
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		return Report{}, fmt.Errorf("audit root %q is not a directory", opts.Root)
	}
	opts.Root = root
	if opts.StateDir == "" {
		opts.StateDir = DefaultStateDir(root)
	}
	if opts.Scope != "" {
		scope := opts.Scope
		if !filepath.IsAbs(scope) {
			scope = filepath.Join(root, scope)
		}
		if rel, relErr := filepath.Rel(root, scope); relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Report{}, fmt.Errorf("scope %q is outside the project", opts.Scope)
		}
		if info, statErr := os.Stat(scope); statErr != nil || !info.IsDir() {
			return Report{}, fmt.Errorf("scope %q is not a directory", opts.Scope)
		}
		opts.Scope = scope
	}

	report := Report{Root: root, Scope: relOrSelf(root, opts.Scope), GeneratedAt: time.Now().UTC()}

	model, err := BuildThreatModel(ctx, root, opts.Scope)
	if err != nil {
		return report, err
	}
	report.ThreatModel = model
	if saveErr := SaveJSON(filepath.Join(opts.StateDir, "threat-model.json"), model); saveErr != nil {
		report.Notes = append(report.Notes, "could not save the threat model: "+saveErr.Error())
	}

	static, err := StaticCandidates(root, opts.Scope)
	if err != nil {
		report.Notes = append(report.Notes, "static analysis failed: "+err.Error())
	}

	if opts.Agent != nil {
		prompt := AuditPrompt(model, static, ManifestPath(opts.StateDir))
		if runErr := opts.Agent.Run(ctx, prompt); runErr != nil {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			report.Notes = append(report.Notes, "agent discovery pass failed: "+runErr.Error())
		}
	} else {
		report.Notes = append(report.Notes, "no agent ran: only the existing findings manifest, static analysis and advisories are reported")
	}

	claimed, err := LoadManifest(ManifestPath(opts.StateDir))
	if err != nil {
		report.Notes = append(report.Notes, "findings manifest ignored: "+err.Error())
	}
	verifier := opts.verifier()
	seen := map[string]bool{}
	for i := range claimed {
		f := &claimed[i]
		if seen[f.ID] {
			f.Status = StatusUnconfirmed
			f.Reason = fmt.Sprintf("duplicate finding id %q", f.ID)
			continue
		}
		seen[f.ID] = true
		verifier.Verify(ctx, f)
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
	}
	report.Findings = append(report.Findings, claimed...)
	report.Findings = append(report.Findings, uncovered(static, claimed)...)

	if opts.Deps != nil {
		res, depErr := opts.Deps(ctx, root)
		if depErr != nil {
			report.Notes = append(report.Notes, "dependency check failed: "+depErr.Error())
		} else {
			deps, notes := DependencyCandidates(res)
			report.Findings = append(report.Findings, deps...)
			report.Notes = append(report.Notes, notes...)
		}
	}
	if model.Languages["go"] == 0 {
		report.Notes = append(report.Notes, "no Go source in scope: proof tests can only be verified for Go, so nothing can be confirmed")
	}

	if opts.Fix {
		if fixErr := applyFixes(ctx, opts, &report); fixErr != nil {
			return report, fixErr
		}
	}

	sortFindings(report.Findings)
	if saveErr := SaveJSON(filepath.Join(opts.StateDir, "report.json"), report); saveErr != nil {
		report.Notes = append(report.Notes, "could not save report.json: "+saveErr.Error())
	}
	if saveErr := os.WriteFile(filepath.Join(opts.StateDir, "report.md"), []byte(RenderMarkdown(report)), 0o644); saveErr != nil {
		report.Notes = append(report.Notes, "could not save report.md: "+saveErr.Error())
	}
	return report, nil
}

// uncovered returns static candidates not already described by an agent
// finding (same file, same CWE, nearby line), so the report does not list
// one weakness twice.
func uncovered(static, claimed []Finding) []Finding {
	var out []Finding
	for _, s := range static {
		covered := false
		for _, c := range claimed {
			if c.CWE == s.CWE && filepath.ToSlash(c.File) == s.File && absInt(c.Line-s.Line) <= 3 {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, s)
		}
	}
	return out
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func relOrSelf(root, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func sortFindings(fs []Finding) {
	group := func(f Finding) int {
		switch f.Status {
		case StatusConfirmed, StatusFixFailed:
			return 0
		case StatusFixed:
			return 1
		}
		return 2
	}
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if group(a) != group(b) {
			return group(a) < group(b)
		}
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() > b.Severity.Rank()
		}
		return a.ID < b.ID
	})
}

// suiteFailures runs the whole module's tests and returns the identifiers
// of everything that failed or did not build, ignoring the named proofs.
func suiteFailures(ctx context.Context, opts Options, ignore map[string]bool) (map[string]bool, error) {
	timeout := opts.SuiteTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	res, err := opts.runner()(ctx, opts.Root, gotest.Options{Packages: "./...", Count: 1, Timeout: timeout})
	if err != nil {
		return nil, err
	}
	failed := map[string]bool{}
	for _, p := range res.Packages {
		if p.BuildError != "" {
			failed[p.Package+" (build)"] = true
		}
	}
	for _, t := range res.Tests {
		if t.Status == gotest.StatusFail && !ignore[t.Name] {
			failed[t.Package+"."+t.Name] = true
		}
	}
	if res.Timeout {
		failed["(suite timeout)"] = true
	}
	return failed, nil
}

// applyFixes asks the agent to fix every confirmed finding, then demands
// three things before it calls anything fixed: the proof file is
// untouched, the proof now passes, and no test that passed before the fix
// fails after it.
func applyFixes(ctx context.Context, opts Options, report *Report) error {
	var targets []int
	for i, f := range report.Findings {
		if f.Status == StatusConfirmed {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		report.Notes = append(report.Notes, "fix requested but no finding is confirmed, so nothing was changed")
		return nil
	}
	if opts.Agent == nil {
		report.Notes = append(report.Notes, "fix requested but no agent is available")
		return nil
	}

	proofNames := map[string]bool{}
	confirmed := make([]Finding, 0, len(targets))
	for _, i := range targets {
		f := report.Findings[i]
		proofNames[f.Proof.Test] = true
		confirmed = append(confirmed, f)
	}

	baseline, err := suiteFailures(ctx, opts, proofNames)
	if err != nil {
		report.Notes = append(report.Notes, "fix skipped: could not run the baseline test suite: "+err.Error())
		return nil
	}

	if runErr := opts.Agent.Run(ctx, FixPrompt(confirmed)); runErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		report.Notes = append(report.Notes, "agent fix pass failed: "+runErr.Error())
	}

	verifier := opts.verifier()
	for _, i := range targets {
		f := &report.Findings[i]
		if verifier.proofTampered(*f) {
			f.Status = StatusFixFailed
			f.Reason = "proof test file was modified or removed during the fix"
			continue
		}
		abs, checkErr := verifier.checkProofFile(*f)
		if checkErr != nil {
			f.Status = StatusFixFailed
			f.Reason = checkErr.Error()
			continue
		}
		outcome, output := verifier.runProof(ctx, *f, abs)
		if outcome == outcomePass {
			f.Status = StatusFixed
			f.Reason = "proof test " + f.Proof.Test + " now passes"
			continue
		}
		f.Status = StatusFixFailed
		switch outcome {
		case outcomeFail:
			f.Reason = "proof test still fails after the fix"
			f.Evidence = clip(trimTestOutput(output), 800)
		case outcomeBuild:
			f.Reason = "proof test no longer compiles: " + output
		default:
			f.Reason = "proof test did not pass after the fix"
		}
	}

	after, err := suiteFailures(ctx, opts, proofNames)
	if err != nil {
		report.Notes = append(report.Notes, "could not re-run the suite after the fix: "+err.Error())
		after = nil
	}
	var regressions []string
	for name := range after {
		if !baseline[name] {
			regressions = append(regressions, name)
		}
	}
	sort.Strings(regressions)
	report.Regressions = regressions
	if err != nil || len(regressions) > 0 {
		reason := "the suite could not be re-run after the fix, so it is not known to be safe"
		if len(regressions) > 0 {
			reason = fmt.Sprintf("the fix broke %d previously passing test(s): %s", len(regressions), strings.Join(firstN(regressions, 5), ", "))
		}
		for _, i := range targets {
			f := &report.Findings[i]
			if f.Status == StatusFixed {
				f.Status = StatusFixFailed
				f.Reason = f.Reason + "; " + reason
			}
		}
	}
	return nil
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
