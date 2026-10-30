package securityaudit

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codeintel"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/depaudit"
)

type kindInfo struct {
	cwe      string
	severity Severity
	title    string
}

// kindTable maps codeintel.SecurityScan kinds to CWE ids. The severities
// are what the pattern alone justifies; a proof test is what raises
// confidence, never the severity.
var kindTable = map[string]kindInfo{
	"hardcoded-credential":   {"CWE-798", SeverityHigh, "Hard-coded credential"},
	"weak-crypto":            {"CWE-327", SeverityMedium, "Use of a broken or risky cryptographic algorithm"},
	"insecure-tls":           {"CWE-295", SeverityHigh, "Certificate verification disabled"},
	"sql-injection-risk":     {"CWE-89", SeverityHigh, "Possible SQL injection"},
	"command-injection-risk": {"CWE-78", SeverityHigh, "Possible OS command injection"},
}

const dependencyCWE = "CWE-1395"

// StaticCandidates runs the existing static scanner over scope and turns
// each hit into an unconfirmed finding. They are leads for the agent and
// for the reader; none can be confirmed without a proof test.
func StaticCandidates(root, scope string) ([]Finding, error) {
	target := root
	if scope != "" {
		target = scope
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
	}
	result, err := codeintel.SecurityScan(target, codeintel.SecurityScanOptions{})
	if err != nil {
		return nil, err
	}
	var out []Finding
	for i, f := range result.Findings {
		info, ok := kindTable[f.Kind]
		if !ok {
			continue
		}
		rel, relErr := filepath.Rel(root, f.File)
		if relErr != nil {
			rel = f.File
		}
		out = append(out, Finding{
			ID:          fmt.Sprintf("S%03d", i+1),
			Title:       info.title,
			CWE:         info.cwe,
			Severity:    info.severity,
			File:        filepath.ToSlash(rel),
			Line:        f.Line,
			Description: f.Message,
			Source:      SourceStatic,
			Status:      StatusUnconfirmed,
			Reason:      "static-analysis candidate with no proof test",
		})
	}
	return out, nil
}

// DependencyScanner reports vulnerable dependencies.
type DependencyScanner func(ctx context.Context, dir string) (depaudit.Result, error)

// DefaultDependencyScanner runs govulncheck through depaudit, skipping the
// slow outdated-module lookup.
func DefaultDependencyScanner(ctx context.Context, dir string) (depaudit.Result, error) {
	return depaudit.Run(ctx, dir, depaudit.Options{SkipUpdates: true})
}

// DependencyCandidates converts vulnerabilities into unconfirmed findings.
// A vulnerability whose affected code is called is ranked high, one that
// merely exists in the module graph low. Both stay unconfirmed: an
// advisory is third-party evidence, not a proof in this project, and the
// report says so.
func DependencyCandidates(res depaudit.Result) ([]Finding, []string) {
	var notes []string
	if !res.VulnToolAvailable {
		msg := res.VulnToolError
		if msg == "" {
			msg = "vulnerability scan did not run"
		}
		notes = append(notes, "dependency check skipped: "+msg)
	}
	var out []Finding
	for i, v := range res.Vulnerabilities {
		sev := SeverityLow
		reach := "affected code is not called from this module"
		if v.Called {
			sev = SeverityHigh
			reach = "affected code is reachable from this module"
			if v.Trace != "" {
				reach += " (" + v.Trace + ")"
			}
		}
		fix := "no fixed version is published"
		if v.Fixed != "" {
			fix = "upgrade " + v.Module + " to " + v.Fixed + " or later"
		}
		out = append(out, Finding{
			ID:          fmt.Sprintf("D%03d", i+1),
			Title:       v.ID + ": " + strings.TrimSpace(v.Summary),
			CWE:         dependencyCWE,
			Severity:    sev,
			File:        "go.mod",
			Description: fmt.Sprintf("%s %s; %s.", v.Module, v.Found, reach),
			Remediation: fix,
			Source:      SourceDependency,
			Status:      StatusUnconfirmed,
			Reason:      "advisory from a vulnerability database; no proof test in this project",
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity.Rank() > out[j].Severity.Rank() })
	return out, notes
}
