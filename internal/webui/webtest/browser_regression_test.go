package webtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

func regressionAssertionScenario() BrowserRegressionScenario {
	return BrowserRegressionScenario{Name: "contract", Workflow: tools.BrowserWorkflowParams{Steps: []tools.BrowserWorkflowStep{
		{ID: "check", Action: "assert", Target: browser.Request{Selector: "#result", Condition: "text", Expected: "done"}},
	}}}
}

func TestBrowserRegressionRejectsRecoveredRequiredFailure(t *testing.T) {
	t.Parallel()
	scenario := regressionAssertionScenario()
	steps, err := EvaluateBrowserRegression(scenario, tools.BrowserRegressionResult{Steps: []tools.PipelineResult{
		{ID: "check", Status: tools.StepFailed, Outcome: tools.OutcomeFailed, IsError: true, Content: "expected done, observed broken"},
		{ID: "recovery", Status: tools.StepSucceeded, Outcome: tools.OutcomeApplied},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "check")
	require.Contains(t, err.Error(), "observed broken")
	require.Len(t, steps, 1)
}

func TestBrowserRegressionRequiresEveryAssertionAndGeneratedVerification(t *testing.T) {
	t.Parallel()
	scenario := regressionAssertionScenario()
	for _, test := range []struct {
		name   string
		result tools.BrowserRegressionResult
	}{
		{name: "false assertion", result: tools.BrowserRegressionResult{Steps: []tools.PipelineResult{{ID: "check", Status: tools.StepFailed, Outcome: tools.OutcomeFailed, IsError: true, ErrorCode: "gate_failed"}}}},
		{name: "missing assertion", result: tools.BrowserRegressionResult{}},
		{name: "skipped assertion", result: tools.BrowserRegressionResult{Steps: []tools.PipelineResult{{ID: "check", Status: tools.StepSkipped, Outcome: tools.OutcomeBlocked}}}},
		{name: "unknown outcome", result: tools.BrowserRegressionResult{Steps: []tools.PipelineResult{{ID: "check", Status: tools.StepUncertain, Outcome: tools.OutcomeUnknown, IsError: true}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := EvaluateBrowserRegression(scenario, test.result)
			require.Error(t, err)
		})
	}
	steps, err := EvaluateBrowserRegression(scenario, tools.BrowserRegressionResult{Steps: []tools.PipelineResult{{ID: "check", Status: tools.StepSucceeded, Outcome: tools.OutcomeVerified}}})
	require.NoError(t, err)
	require.Equal(t, tools.StepSucceeded, steps[0].Status)
}

func TestBrowserRegressionDoesNotTrustExtraSuccessfulSteps(t *testing.T) {
	t.Parallel()
	scenario := regressionAssertionScenario()
	_, err := EvaluateBrowserRegression(scenario, tools.BrowserRegressionResult{Steps: []tools.PipelineResult{
		{ID: "check", Status: tools.StepSucceeded, Outcome: tools.OutcomeVerified},
		{ID: "alternate-route", Status: tools.StepSucceeded, Outcome: tools.OutcomeVerified},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected exactly 1")
}

func TestBrowserRegressionReportsAndRedactsFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "junit.xml")
	redact := regressionRedactor("contract-test", []string{"plain-secret"})
	report := BrowserRegressionReport{Name: "contract", Failure: "password=plain-secret token=abc123", Diagnostic: "cookie: session-value"}
	require.NoError(t, WriteBrowserRegressionJUnit(path, report, redact))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "failures=\"1\"")
	require.NotContains(t, text, "plain-secret")
	require.NotContains(t, text, "abc123")
	require.NotContains(t, text, "session-value")
}
