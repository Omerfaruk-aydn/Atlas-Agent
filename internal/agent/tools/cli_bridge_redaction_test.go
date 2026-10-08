package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCLIBridgeBackgroundProtectsCapturedEnvironment(t *testing.T) {
	// Process environment mutation requires this test to remain serial.
	secret := "redaction-original-" + uuid.NewString() + "\"\\ç\nend"
	t.Setenv("CLI_BRIDGE_FIXTURE_TOKEN", secret)
	op := fixtureOperation("env-both", []any{"env-both", "CLI_BRIDGE_FIXTURE_TOKEN"}, nil,
		map[string]any{"background": true, "stdout_contains": secret})
	profile := fixtureBridgeProfile(t, "", op)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(profile, &doc))
	doc["env"] = []any{"CLI_BRIDGE_FIXTURE_TOKEN"}
	profile, err := json.Marshal(doc)
	require.NoError(t, err)
	h := newBridgeHarness(t, true, CommandPolicy{}, profile)
	h.bash = WithSecretFilter(h.bash)
	resp, err := h.call(t, h.ctx, CLIBridgeParams{Action: "run", Profile: "fixture", Operation: "env-both", Background: true})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var running clibridge.RunReport
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &running))
	require.Equal(t, clibridge.OutcomeRunning, running.Outcome)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	job, ok := shell.GetBackgroundShellManager().Get(running.JobID)
	require.True(t, ok)
	require.True(t, job.WaitContext(ctx))
	// The job inherited the original value; the parent changes before collection.
	t.Setenv("CLI_BRIDGE_FIXTURE_TOKEN", "changed-parent-environment")
	jobInput, err := json.Marshal(JobOutputParams{ShellID: running.JobID})
	require.NoError(t, err)
	jobResp, err := WithSecretFilter(NewJobOutputTool()).Run(h.ctx, fantasy.ToolCall{ID: "job-check", Input: string(jobInput)})
	require.NoError(t, err)
	require.NotContains(t, jobResp.Content, "redaction-original-")
	require.NotContains(t, jobResp.Metadata, "redaction-original-")

	for range 2 {
		resp, err = h.call(t, h.ctx, CLIBridgeParams{Action: "status", RunID: running.RunID})
		require.NoError(t, err)
		require.False(t, resp.IsError, resp.Content)
		require.NotContains(t, resp.Content, "redaction-original-")
		require.NoError(t, json.Unmarshal([]byte(resp.Content), &running))
		require.True(t, running.Passed, "Expectations must use original output before redaction")
		require.Contains(t, running.Stdout, "[REDACTED]")
		require.Contains(t, running.Stderr, "[REDACTED]")
		require.Positive(t, running.StdoutBytes)
		require.Positive(t, running.StderrBytes)
	}
	_, raw := h.stored(t, running.RunID)
	require.NotContains(t, raw, "redaction-original-")
	require.Contains(t, raw, "[REDACTED]")
}

func TestCLIBridgeProtectedDispatchPreservesEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		secret     string
		background bool
	}{
		{"foreground", "evidence-original-" + uuid.NewString(), false},
		{"short-background", "abc", true},
		{"boolean-background", "true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLI_BRIDGE_FIXTURE_TOKEN", tc.secret)
			op := fixtureOperation("env-both", []any{"env-both", "CLI_BRIDGE_FIXTURE_TOKEN"}, nil,
				map[string]any{"background": true, "stdout_contains": tc.secret})
			profile := fixtureBridgeProfile(t, "", op)
			var doc map[string]any
			require.NoError(t, json.Unmarshal(profile, &doc))
			doc["env"] = []any{"CLI_BRIDGE_FIXTURE_TOKEN"}
			profile, err := json.Marshal(doc)
			require.NoError(t, err)
			h := newBridgeHarness(t, true, CommandPolicy{}, profile)
			h.bash = WithSecretFilter(h.bash)
			resp, err := h.call(t, h.ctx, CLIBridgeParams{Action: "run", Profile: "fixture", Operation: "env-both", Background: tc.background})
			require.NoError(t, err)
			require.False(t, resp.IsError, resp.Content)
			var report clibridge.RunReport
			require.NoError(t, json.Unmarshal([]byte(resp.Content), &report))
			if tc.background {
				job, ok := shell.GetBackgroundShellManager().Get(report.JobID)
				require.True(t, ok)
				ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
				defer cancel()
				require.True(t, job.WaitContext(ctx))
				resp, err = h.call(t, h.ctx, CLIBridgeParams{Action: "status", RunID: report.RunID})
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal([]byte(resp.Content), &report))
				jobInput, err := json.Marshal(JobOutputParams{ShellID: report.JobID})
				require.NoError(t, err)
				output, err := WithSecretFilter(NewJobOutputTool()).Run(h.ctx, fantasy.ToolCall{ID: "protected-job", Input: string(jobInput)})
				require.NoError(t, err)
				require.True(t, json.Valid([]byte(output.Metadata)))
				require.NotContains(t, output.Content, "value="+tc.secret)
			}
			require.True(t, report.Passed, report.Detail)
			require.Equal(t, 0, *report.ExitCode)
			require.NotContains(t, report.Stdout, tc.secret)
			require.Contains(t, report.Stdout, "value=[REDACTED]")
			_, raw := h.stored(t, report.RunID)
			require.NotContains(t, raw, "value="+tc.secret)
			require.Contains(t, raw, "[REDACTED]")
		})
	}
}

func TestCLIBridgeRefusesExecutionWithoutSecretProtection(t *testing.T) {
	t.Parallel()
	h := newBridgeHarness(t, true, CommandPolicy{}, nil)
	session := "exhausted-" + uuid.NewString()
	h.ctx = context.WithValue(h.ctx, SessionIDContextKey, session)
	for i := range 128 {
		require.NoError(t, vault.Register(session, fmt.Sprintf("protected-budget-%s-%d", session, i)))
	}
	resp, err := h.call(t, h.ctx, CLIBridgeParams{Action: "run", Profile: "fixture", Operation: "secret", Args: []string{"token=unregistered-" + uuid.NewString()}})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "credential protection")
	require.Zero(t, h.starts.Load(), "No process may start without secret protection")
}

func TestCLIBridgeRedactionKeepsNativeReferences(t *testing.T) {
	t.Parallel()
	session := "reference-" + uuid.NewString()
	require.NoError(t, vault.Register(session, "abc"))
	require.NoError(t, vault.Register(session, "ABC"))
	require.NoError(t, vault.Register(session, string(clibridge.OutcomeRunning)))
	report := clibridge.RunReport{
		RunID:     "run-abc00000-0000-4000-8000-000000000000",
		PrepareID: "prep-abc000000000000000000000",
		JobID:     "ABC", Stdout: "value=abc ABC running", Outcome: clibridge.OutcomeRunning,
	}
	sanitized := redactRegisteredBridgeReport(session, report)
	require.Equal(t, report.RunID, sanitized.RunID)
	require.Equal(t, report.PrepareID, sanitized.PrepareID)
	require.Equal(t, report.JobID, sanitized.JobID)
	require.Equal(t, clibridge.OutcomeRunning, sanitized.Outcome)
	require.Equal(t, "value=[REDACTED] [REDACTED] [REDACTED]", sanitized.Stdout)

	ctx := context.WithValue(t.Context(), SessionIDContextKey, session)
	tool := fantasy.NewAgentTool(CLIBridgeToolName, "Native reference fixture.", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return jsonResponse(report, CLIBridgeMetadata{RunID: report.RunID, Prepared: true}, false)
	})
	response, err := WithSecretFilter(tool).Run(ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	var got clibridge.RunReport
	require.NoError(t, json.Unmarshal([]byte(response.Content), &got))
	require.Equal(t, report.RunID, got.RunID)
	require.Equal(t, report.PrepareID, got.PrepareID)
	require.Equal(t, report.JobID, got.JobID)
	require.Equal(t, report.Outcome, got.Outcome)
	require.Equal(t, sanitized.Stdout, got.Stdout)
	var metadata CLIBridgeMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.Equal(t, report.RunID, metadata.RunID)

	previewTool := fantasy.NewAgentTool(CLIBridgeToolName, "Preview reference fixture.", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return prepared(map[string]any{"preview": clibridge.Prepared{ID: report.PrepareID, StdoutContains: "abc"}})
	})
	preview, err := WithSecretFilter(previewTool).Run(ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	var gotPreview struct {
		Preview clibridge.Prepared `json:"preview"`
	}
	require.NoError(t, json.Unmarshal([]byte(preview.Content), &gotPreview))
	require.Equal(t, report.PrepareID, gotPreview.Preview.ID)
	require.Equal(t, "[REDACTED]", gotPreview.Preview.StdoutContains)

	invalid := `{"run_id":"abc","output":{"prepare_id":"prep-abc000000000000000000000"}}`
	protected := redactCLIBridgePayload(session, invalid)
	require.NotContains(t, protected, "abc")
}

func TestCLIBridgeBackgroundReferencesSurviveProtection(t *testing.T) {
	t.Parallel()
	op := fixtureOperation("echo", []any{"echo", "payload"}, nil, map[string]any{"background": true})
	h := newBridgeHarness(t, true, CommandPolicy{}, fixtureBridgeProfile(t, "", op))
	h.ctx = context.WithValue(h.ctx, SessionIDContextKey, "bridge-reference-"+uuid.NewString())
	h.tool = WithSecretFilter(h.tool)
	h.bash = WithSecretFilter(h.bash)
	preview, err := h.call(t, h.ctx, CLIBridgeParams{Action: "preview", Profile: "fixture", Operation: "echo"})
	require.NoError(t, err)
	require.False(t, preview.IsError, preview.Content)
	var preparation struct {
		Preview clibridge.Prepared `json:"preview"`
	}
	require.NoError(t, json.Unmarshal([]byte(preview.Content), &preparation))
	// Force an identifier collision rather than relying on a random hash.
	id := preparation.Preview.ID
	require.GreaterOrEqual(t, len(id), 8)
	require.NoError(t, vault.Register(GetSessionFromContext(h.ctx), id[len(id)-8:]))
	require.NoError(t, vault.Register(GetSessionFromContext(h.ctx), h.root))
	response, err := h.call(t, h.ctx, CLIBridgeParams{Action: "run", Profile: "fixture", Operation: "echo", PrepareID: id, Background: true})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	var report clibridge.RunReport
	require.NoError(t, json.Unmarshal([]byte(response.Content), &report))
	require.Equal(t, id, report.PrepareID)
	require.Equal(t, "[REDACTED]", report.WorkingDir)
	job, ok := shell.GetBackgroundShellManager().Get(report.JobID)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Second)
	defer cancel()
	require.True(t, job.WaitContext(ctx))
	response, err = h.call(t, h.ctx, CLIBridgeParams{Action: "status", RunID: report.RunID})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.NoError(t, json.Unmarshal([]byte(response.Content), &report))
	require.True(t, report.Passed, report.Detail)
	require.Equal(t, id, report.PrepareID)
	require.Equal(t, "[REDACTED]", report.WorkingDir)
}
