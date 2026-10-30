// Package securityaudit runs a defensive security audit whose findings
// are confirmed by evidence rather than by assertion.
//
// A language model that is asked for vulnerabilities will name plausible
// ones, and a report of plausible vulnerabilities trains its readers to
// skim. The rule enforced here is that a finding is "confirmed" only when
// a proof test in the user's own project fails for the reason the finding
// claims, on every run. Everything else is reported as "unconfirmed" and
// is never presented as confirmed, however severe it sounds.
//
// The package is deliberately free of any model client. The agent writes
// proof tests and a findings manifest; this package validates the
// manifest, runs the proofs, assigns the status, and, in fix mode, checks
// that the proof turns green while the rest of the suite stays green.
package securityaudit

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Severity ranks how bad a confirmed finding is.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Rank orders severities, higher is worse. Unknown values rank zero.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// ParseSeverity accepts a severity name in any case.
func ParseSeverity(v string) (Severity, error) {
	s := Severity(strings.ToLower(strings.TrimSpace(v)))
	if s.Rank() == 0 {
		return "", fmt.Errorf("unknown severity %q (use critical, high, medium, low or info)", v)
	}
	return s, nil
}

// Status is how far a finding has been established.
type Status string

const (
	// StatusConfirmed means a proof test fails, repeatably, naming the
	// finding.
	StatusConfirmed Status = "confirmed"
	// StatusUnconfirmed means there is no passing evidence: no proof, a
	// proof that does not compile, or a proof that does not fail.
	StatusUnconfirmed Status = "unconfirmed"
	// StatusFixed means the proof turned green after a fix and the rest
	// of the suite did not regress.
	StatusFixed Status = "fixed"
	// StatusFixFailed means a fix was attempted and did not hold.
	StatusFixFailed Status = "fix_failed"
)

// Source says where a finding came from.
type Source string

const (
	SourceAgent      Source = "agent"
	SourceStatic     Source = "static"
	SourceDependency Source = "dependency"
)

// Proof points at the test that demonstrates a finding.
type Proof struct {
	// File is the _test.go file, relative to the project root.
	File string `json:"file"`
	// Test is the top-level test function name.
	Test string `json:"test"`
	// SHA256 is the digest of File when the finding was confirmed. A fix
	// may not change the proof: a "fix" that edits the test until it
	// passes proves nothing.
	SHA256 string `json:"sha256,omitempty"`
}

// Finding is one potential vulnerability.
type Finding struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	CWE      string   `json:"cwe"`
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	// Description says what is wrong.
	Description string `json:"description,omitempty"`
	// AttackPath describes what the attacker controls and what they gain.
	AttackPath string `json:"attack_path,omitempty"`
	// Remediation is the recommended fix.
	Remediation string `json:"remediation,omitempty"`
	Proof       *Proof `json:"proof,omitempty"`
	Source      Source `json:"source,omitempty"`
	// Status, Reason and Evidence are always computed here. A value
	// supplied in the manifest is discarded, so the agent cannot declare
	// its own findings confirmed.
	Status   Status `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)
	cwePattern  = regexp.MustCompile(`^CWE-[0-9]{1,5}$`)
	testPattern = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
)

// Signal is one place the threat-model scan noticed something relevant.
type Signal struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Detail string `json:"detail,omitempty"`
}

// ThreatModel is the deterministic starting point handed to the agent.
type ThreatModel struct {
	Languages map[string]int `json:"languages"`
	Files     int            `json:"files"`
	// EntryPoints are where outside data enters the program.
	EntryPoints []Signal `json:"entry_points"`
	// Sinks are operations that become dangerous with attacker data.
	Sinks []Signal `json:"sinks"`
	// Hotspots are the files with the most signals, worth reading first.
	Hotspots    []string `json:"hotspots"`
	Boundaries  []string `json:"trust_boundaries"`
	Assumptions []string `json:"assumptions"`
	// Truncated reports that signal lists were capped.
	Truncated bool `json:"truncated,omitempty"`
}

// Report is the outcome of an audit.
type Report struct {
	Root        string      `json:"root"`
	Scope       string      `json:"scope,omitempty"`
	GeneratedAt time.Time   `json:"generated_at"`
	ThreatModel ThreatModel `json:"threat_model"`
	Findings    []Finding   `json:"findings"`
	// Regressions lists tests that passed before a fix and failed after.
	Regressions []string `json:"regressions,omitempty"`
	// Notes carry limits and tool availability, e.g. a missing
	// govulncheck. An audit that could not run a check says so.
	Notes []string `json:"notes,omitempty"`
}

// Count returns how many findings carry the status.
func (r Report) Count(s Status) int {
	n := 0
	for _, f := range r.Findings {
		if f.Status == s {
			n++
		}
	}
	return n
}

// Blocking returns findings that are confirmed and still open (or whose
// fix failed) at or above the minimum severity.
func (r Report) Blocking(minimum Severity) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Status == StatusConfirmed || f.Status == StatusFixFailed {
			if f.Severity.Rank() >= minimum.Rank() {
				out = append(out, f)
			}
		}
	}
	return out
}
