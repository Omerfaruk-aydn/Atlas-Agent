package webtest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

// BrowserRegressionScenario is deliberately data-only: fixed scenarios need
// no model decision and cannot introduce arbitrary script execution.
type BrowserRegressionScenario struct {
	Name     string                      `json:"name"`
	Secrets  []string                    `json:"secrets,omitempty"`
	Workflow tools.BrowserWorkflowParams `json:"workflow"`
}

type BrowserRegressionStep struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Outcome   string `json:"outcome"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
}

type BrowserRegressionReport struct {
	Name       string                  `json:"name"`
	Passed     bool                    `json:"passed"`
	DurationMS int64                   `json:"duration_ms"`
	Steps      []BrowserRegressionStep `json:"steps"`
	Failure    string                  `json:"failure,omitempty"`
	Screenshot string                  `json:"screenshot,omitempty"`
	Diagnostic string                  `json:"diagnostic,omitempty"`
}

// EvaluateBrowserRegression applies the acceptance contract independently of
// the pipeline's recovery semantics. Every compiled action and verification
// must have succeeded; skipped, unknown, failed and recovered failures fail.
func EvaluateBrowserRegression(scenario BrowserRegressionScenario, result tools.BrowserRegressionResult) ([]BrowserRegressionStep, error) {
	compiled, err := tools.CompileBrowserRegressionWorkflow(scenario.Workflow)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]tools.PipelineResult, len(result.Steps))
	for _, step := range result.Steps {
		if _, duplicate := byID[step.ID]; duplicate {
			return nil, fmt.Errorf("duplicate step %q in runner ledger", step.ID)
		}
		byID[step.ID] = step
	}
	steps := make([]BrowserRegressionStep, 0, len(compiled))
	var failures []string
	for _, required := range compiled {
		actual, found := byID[required.ID]
		step := BrowserRegressionStep{ID: required.ID, Expected: "status=succeeded"}
		if !found {
			step.Status = "missing"
			step.Actual = "no ledger entry"
		} else {
			step.Status, step.Outcome, step.ErrorCode, step.Attempts = actual.Status, actual.Outcome, actual.ErrorCode, actual.Attempts
			step.Actual = "status=" + actual.Status + ", outcome=" + actual.Outcome
			if actual.IsError || actual.Status != tools.StepSucceeded {
				step.Actual += ", reason=" + firstNonEmptyRegression(actual.Reason, actual.Content)
			}
		}
		if !found || actual.IsError || actual.Status != tools.StepSucceeded {
			failures = append(failures, required.ID+": expected status=succeeded; observed "+step.Actual)
		}
		steps = append(steps, step)
	}
	if len(result.Steps) != len(compiled) {
		failures = append(failures, fmt.Sprintf("ledger has %d entries; expected exactly %d", len(result.Steps), len(compiled)))
	}
	if len(failures) > 0 {
		return steps, errors.New(strings.Join(failures, "; "))
	}
	return steps, nil
}

func firstNonEmptyRegression(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "not reported"
}

var regressionSecretPattern = regexp.MustCompile(`(?i)(authorization|cookie|token|secret|password|api[_-]?key)(\s*[:=]\s*)([^\s,;]+)`)

func regressionRedactor(sessionID string, secrets []string) func(string) string {
	return func(value string) string {
		value = vault.Redact(sessionID, value)
		for _, secret := range secrets {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
			}
		}
		return regressionSecretPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	}
}

// RunBrowserRegression owns only the supplied isolated manager. Failure
// artifacts contain the full redacted step ledger and, where supported, a
// clean protected screenshot. Screenshot failure is reported, never ignored.
func RunBrowserRegression(ctx context.Context, sessions tools.BrowserRegressionSessions, workingDir, artifactDir string, scenario BrowserRegressionScenario) BrowserRegressionReport {
	started := time.Now()
	report := BrowserRegressionReport{Name: scenario.Name}
	redact := regressionRedactor("web-regression", scenario.Secrets)
	for _, secret := range scenario.Secrets {
		if secret != "" {
			_ = vault.Register("web-regression", secret)
		}
	}
	result, runErr := tools.RunBrowserRegression(ctx, sessions, workingDir, "web-regression", scenario.Workflow)
	if runErr != nil {
		report.Failure = runErr.Error()
	} else {
		report.Steps, runErr = EvaluateBrowserRegression(scenario, result)
		if runErr != nil {
			report.Failure = runErr.Error()
		}
	}
	report.Passed = runErr == nil
	report.DurationMS = time.Since(started).Milliseconds()
	if !report.Passed {
		if session, err := sessions.Session("web-regression"); err != nil {
			report.Diagnostic = "screenshot unavailable: " + err.Error()
		} else if protected, ok := session.(browser.ProtectedScreenshotSession); !ok {
			report.Diagnostic = "screenshot unavailable: driver does not support protected capture"
		} else if data, _, err := protected.ProtectedScreenshot(ctx, false, false, redact); err != nil {
			report.Diagnostic = "screenshot unavailable: " + err.Error()
		} else if err := os.MkdirAll(artifactDir, 0o755); err != nil {
			report.Diagnostic = "artifact directory unavailable: " + err.Error()
		} else {
			name := filepath.Join(artifactDir, "failure.png")
			if err := os.WriteFile(name, data, 0o600); err != nil {
				report.Diagnostic = "screenshot write failed: " + err.Error()
			} else {
				report.Screenshot = name
			}
		}
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		report.Passed = false
		report.Failure = firstNonEmptyRegression(report.Failure, "artifact directory unavailable") + ": " + err.Error()
	} else {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		clean := redact(string(encoded))
		if err := os.WriteFile(filepath.Join(artifactDir, "report.json"), []byte(clean), 0o600); err != nil {
			report.Passed = false
			report.Failure = firstNonEmptyRegression(report.Failure, "report write failed") + ": " + err.Error()
		}
	}
	return report
}

type regressionJUnit struct {
	XMLName  xml.Name              `xml:"testsuite"`
	Name     string                `xml:"name,attr"`
	Tests    int                   `xml:"tests,attr"`
	Failures int                   `xml:"failures,attr"`
	Time     string                `xml:"time,attr"`
	Cases    []regressionJUnitCase `xml:"testcase"`
}

type regressionJUnitCase struct {
	Name    string `xml:"name,attr"`
	Time    string `xml:"time,attr"`
	Failure *struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	} `xml:"failure,omitempty"`
}

// WriteBrowserRegressionJUnit returns an error on write failure so the CLI
// can still produce a nonzero exit rather than losing the only CI report.
func WriteBrowserRegressionJUnit(path string, report BrowserRegressionReport, redact func(string) string) error {
	failures := 0
	if !report.Passed {
		failures = 1
	}
	suite := regressionJUnit{Name: "atlas-browser-use", Tests: 1, Failures: failures, Time: fmt.Sprintf("%.3f", float64(report.DurationMS)/1000), Cases: []regressionJUnitCase{{Name: report.Name, Time: fmt.Sprintf("%.3f", float64(report.DurationMS)/1000)}}}
	if !report.Passed {
		suite.Cases[0].Failure = &struct {
			Message string `xml:"message,attr"`
			Text    string `xml:",chardata"`
		}{Message: "browser regression failed", Text: regressionSecretPattern.ReplaceAllString(redact(report.Failure+"; "+report.Diagnostic), "$1$2[REDACTED]")}
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0o600)
}
