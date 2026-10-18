package agent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

func writeWorkPlan(t *testing.T, root, plan string) {
	t.Helper()
	dir := filepath.Join(root, workplan.Dir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, workplan.PlanFile), []byte(plan), 0o644))
}

// fakeBashTool exits 0 unless the command contains "fail", and reports the
// exit code in metadata the way the real bash tool does.
func fakeBashTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("bash", "Run a command.", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		exit := 0
		output := "ok"
		if strings.Contains(p.Command, "fail") {
			exit, output = 1, "FAIL TestCache"
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(output), tools.BashResponseMetadata{ExitCode: &exit}), nil
	})
}

func bashStep(id, command string, exit int) []fantasy.Content {
	meta := `{"exit_code":` + strconv.Itoa(exit) + `}`
	return []fantasy.Content{
		fantasy.ToolCallContent{ToolCallID: id, ToolName: "bash", Input: `{"command":"` + command + `"}`},
		fantasy.ToolResultContent{ToolCallID: id, ToolName: "bash", Result: fantasy.ToolResultOutputContentText{Text: "out"}, ClientMetadata: meta},
	}
}

func editStep(id, path string) []fantasy.Content {
	return []fantasy.Content{
		fantasy.ToolCallContent{ToolCallID: id, ToolName: "edit", Input: `{"file_path":"` + path + `"}`},
		fantasy.ToolResultContent{ToolCallID: id, ToolName: "edit", Result: fantasy.ToolResultOutputContentText{Text: "ok"}},
	}
}

func steps(parts ...[]fantasy.Content) []fantasy.StepResult {
	var content fantasy.ResponseContent
	for _, p := range parts {
		content = append(content, p...)
	}
	return []fantasy.StepResult{{Response: fantasy.Response{Content: content}}}
}

func TestContractGapsJudgeTheLatestRunAfterTheLastEdit(t *testing.T) {
	t.Parallel()

	reqs := []workplan.Requirement{{Command: "go test ./...", Reason: "contract check unit"}}
	cases := []struct {
		name string
		run  []fantasy.StepResult
		want string
	}{
		{"not run", steps(editStep("1", "a.go")), "not run"},
		{"stale", steps(bashStep("1", "go test ./...", 0), editStep("2", "a.go")), "before the latest code change"},
		{"failed", steps(editStep("1", "a.go"), bashStep("2", "go test ./...", 1)), "failed"},
		{"passed then plan updated", steps(editStep("1", "a.go"), bashStep("2", "cd app && go test ./...", 0), editStep("3", ".atlas/work.json")), ""},
		{"failed then fixed", steps(editStep("1", "a.go"), bashStep("2", "go test ./...", 1), editStep("3", "a.go"), bashStep("4", "go test ./...", 0)), ""},
		{"exit status discarded", steps(editStep("1", "a.go"), bashStep("2", "go test ./... || true", 0)), "not run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var l evaluatorLedger
			l.record(tc.run)
			gaps := l.contractGaps(reqs)
			if tc.want == "" {
				require.Empty(t, gaps)
				return
			}
			require.Len(t, gaps, 1)
			require.Contains(t, gaps[0].String(), tc.want)
			require.Contains(t, gaps[0].String(), "contract check unit")
		})
	}
}

func TestPlanEditsAloneDoNotOpenTheGate(t *testing.T) {
	t.Parallel()

	var l evaluatorLedger
	l.record(steps(editStep("1", ".atlas/progress.md"), editStep("2", "proj/.atlas/work.json")))
	require.False(t, l.changed)
	require.Empty(t, l.files)
}

func TestContractRemembersEarlyRunsBeyondTheReviewWindow(t *testing.T) {
	t.Parallel()

	var l evaluatorLedger
	parts := [][]fantasy.Content{editStep("e", "a.go"), bashStep("v", "make verify", 0)}
	for i := range evaluatorMaxCommands + 5 {
		parts = append(parts, bashStep("n"+strconv.Itoa(i), "ls "+strconv.Itoa(i), 0))
	}
	l.record(steps(parts...))
	require.Len(t, l.commands, evaluatorMaxCommands)
	require.Empty(t, l.contractGaps([]workplan.Requirement{{Command: "make verify", Reason: "r"}}))
}

// Without any reviewer model, a turn that changed code but skipped the
// contract is sent back, and passes once the check has run.
func TestContractGateSendsUnverifiedWorkBack(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Done."},
		{tool: "bash", input: `{"command":"go test ./...","description":"Run tests"}`},
		{text: "go test ./... passed."},
	}}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool(), fakeBashTool())
	writeWorkPlan(t, a.workingDir, `{"goal":"cache","verify":[{"name":"unit","command":"go test ./..."}]}`)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "add the cache", MaxOutputTokens: 1000})
	require.NoError(t, err)

	calls := model.calls()
	require.Len(t, calls, 4)
	require.Contains(t, requestText(calls[0]), "<work_plan", "the plan is shown at the start of the request")
	require.Contains(t, requestText(calls[2]), "verification contract")
	require.Contains(t, requestText(calls[2]), "`go test ./...` (contract check unit): not run")
}

// A check that keeps failing ends the turn after the round limit with the
// failure recorded, instead of looping on the user's budget.
func TestContractGateStopsAtRoundLimit(t *testing.T) {
	t.Parallel()

	script := []scriptedStep{{tool: "edit", input: `{"file_path":"a.go"}`}}
	// Each attempt varies its flags, as a real agent's would; identical
	// repeated failures are stopped earlier by the loop detector.
	for i := range contractMaxRounds + 1 {
		command := `{"command":"make fail V=` + strconv.Itoa(i) + `","description":"Run checks"}`
		script = append(script, scriptedStep{tool: "bash", input: command}, scriptedStep{text: "Done."})
	}
	model := &scriptedModel{steps: script}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool(), fakeBashTool())
	writeWorkPlan(t, a.workingDir, `{"verify":[{"command":"make fail"}]}`)
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "fix it", MaxOutputTokens: 1000})
	require.NoError(t, err)

	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var last *message.Message
	for i := range msgs {
		if msgs[i].Role == message.Assistant {
			last = &msgs[i]
		}
	}
	require.NotNil(t, last)
	finish := last.FinishPart()
	require.NotNil(t, finish)
	require.Len(t, model.calls(), len(script))
	require.Contains(t, finish.Details, "Verification contract not met")
	require.Contains(t, finish.Details, "make fail")
}

// No plan means no contract: the turn ends as before.
func TestContractGateIsSilentWithoutAPlan(t *testing.T) {
	t.Parallel()

	model := &scriptedModel{steps: []scriptedStep{
		{tool: "edit", input: `{"file_path":"a.go"}`},
		{text: "Done."},
	}}
	a, env, _ := newWorkLoopAgent(t, model, fakeEditTool())
	sess, err := env.sessions.Create(t.Context(), "s")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "rename it", MaxOutputTokens: 1000})
	require.NoError(t, err)
	require.Len(t, model.calls(), 2)
	require.NotContains(t, requestText(model.calls()[0]), "<work_plan")
}
