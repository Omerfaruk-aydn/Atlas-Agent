package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"

	"github.com/stretchr/testify/require"
)

func TestStructuredReviewRejectsUnsupportedAndContradictoryReports(t *testing.T) {
	t.Parallel()
	for _, text := range []string{`VERDICT: PASS`, `{"verdict":"fail","findings":[]}`, `{"verdict":"pass","findings":[{"severity":"high","file":"a.go","line":1,"summary":"broken","evidence":"test failed"}]}`, `{"verdict":"pass","findings":[],"surprise":true}`, `{"verdict":"pass","findings":[]} trailing`} {
		_, err := decodeReview(text)
		require.Error(t, err, text)
	}
	r, err := decodeReview(`{"verdict":"fail","findings":[{"severity":"high","file":"a.go","line":3,"summary":"nil map write","evidence":"TestCache fails at a.go:3"}]}`)
	require.NoError(t, err)
	require.Contains(t, r.findingsText(), "a.go:3 nil map write")
}

func TestEvaluatorAcceptsSchemaValidatedToolVerdict(t *testing.T) {
	t.Parallel()
	info := newSubmitReviewTool().Info()
	require.Equal(t, submitReviewToolName, info.Name)
	require.Contains(t, info.Required, "verdict")
	require.Contains(t, info.Required, "findings")
	const verdict = `{"verdict":"fail","findings":[{"severity":"high","file":"a.go","line":3,"summary":"nil map write","evidence":"TestCache fails at a.go:3"}]}`
	reviewer := &scriptedModel{steps: []scriptedStep{{tool: submitReviewToolName, input: verdict}, {text: "review submitted"}}}
	a, _, _ := newWorkLoopAgent(t, &scriptedModel{})
	withEvaluator(a, reviewer, 2)
	reply, err := a.runEvaluator(t.Context(), "structured-tool", "review the change", nil)
	require.NoError(t, err)
	require.JSONEq(t, verdict, reply)
}

func TestShellEditIsCapturedAgainstDirtyRequestBaseline(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	path := filepath.Join(env.workingDir, "fixture.go")
	require.NoError(t, os.WriteFile(path, []byte("package user_dirty\n"), 0o600))
	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "shell", Prompt: "fix fixture"})
	a.beginProjectEvidence(t.Context(), "shell")
	require.NoError(t, os.WriteFile(path, []byte("package fixed\n"), 0o600))
	a.syncProjectEvidence(t.Context(), "shell")
	l := a.evaluatorLedgerFor("shell")
	l.mu.Lock()
	defer l.mu.Unlock()
	require.True(t, l.changed)
	require.Equal(t, "package user_dirty\n", l.baselines[path].content)
	require.Contains(t, l.files, "fixture.go")
}

func TestFailedCheapStageDoesNotInvokeReviewer(t *testing.T) {
	t.Parallel()
	writer := &scriptedModel{steps: []scriptedStep{{tool: "edit", input: `{"file_path":"a.go"}`}, {text: "done"}, {text: "still failed"}, {text: "unresolved"}}}
	checks := fantasy.NewAgentTool("bash", "fixture", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		exit := 1
		meta, _ := json.Marshal(tools.BashResponseMetadata{ExitCode: &exit, Executed: true})
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("--- FAIL: TestBehavior"), json.RawMessage(meta)), nil
	})
	a, env, _ := newWorkLoopAgent(t, writer, fakeEditTool(), checks)
	reviewer := &scriptedModel{repeat: &scriptedStep{text: `{"verdict":"pass","findings":[]}`}}
	withEvaluator(a, reviewer, 2)
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{CompletionCommands: []string{"go test ./..."}}}}).Scoped(env.workingDir)
	sess, err := env.sessions.Create(t.Context(), "cheap")
	require.NoError(t, err)
	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "fix the behavior"})
	require.NoError(t, err)
	require.Empty(t, reviewer.calls())
}

func TestNoProgressEscalatesWithoutMistakingReadsForProgress(t *testing.T) {
	t.Parallel()
	m := &strategyMonitor{noProgressLimit: 2}
	var steps []fantasy.StepResult
	for i := 0; i < 8; i++ {
		steps = append(steps, fantasy.StepResult{})
		stopped := m.observe(t.Context(), nil, "s", steps, "same-semantic-progress")
		if i < 6 {
			require.False(t, stopped)
		}
	}
	require.NotEmpty(t, m.stoppedReason())
	require.Equal(t, blockedVerdict, decideCompletion(unifiedInput{Blocker: m.stoppedReason()}).Verdict)
}

func TestEvaluatorRepairsInvalidJSONOnceAndNeverCertifiesInvalidReply(t *testing.T) {
	t.Parallel()
	for _, repair := range []string{`{"verdict":"pass","findings":[]}`, "still invalid"} {
		a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
		enabled := true
		a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{EvaluatorRepair: &enabled}}}).Scoped(env.workingDir)
		reviewer := &scriptedModel{steps: []scriptedStep{{text: "invalid"}, {text: repair}}}
		withEvaluator(a, reviewer, 2)
		sess, err := env.sessions.Create(t.Context(), "schema")
		require.NoError(t, err)
		l := a.evaluatorLedgerFor(sess.ID)
		l.changed = true
		result := a.evaluateTurn(t.Context(), sess.ID, "done")
		require.Len(t, reviewer.calls(), 2)
		if repair == "still invalid" {
			require.Equal(t, reviewInvalidReport, result.Review)
			require.NotEmpty(t, result.Unverified)
		} else {
			require.Equal(t, reviewPassed, result.Review)
		}
	}
}

func TestRedEvidenceRequiresSameCheckAndInterveningEdit(t *testing.T) {
	t.Parallel()
	l := &evaluatorLedger{}
	l.observeRedGreen(evaluatorCommand{command: "go test ./...", failed: true, seq: 1})
	l.lastEdit = 2
	l.observeRedGreen(evaluatorCommand{command: "go build ./...", seq: 3})
	require.Empty(t, l.redGreen)
	l.observeRedGreen(evaluatorCommand{command: "go test ./...", seq: 4})
	require.Len(t, l.redGreen, 1)
	require.Equal(t, 1, l.redGreen[0].RedSequence)
	require.Equal(t, 4, l.redGreen[0].GreenSequence)
}

func TestRedGreenMustProveAContractRequirement(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	enabled := true
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{RedGreen: &enabled, CompletionCommands: []string{"go test ./..."}}}}).Scoped(env.workingDir)
	plan := []byte(`{"features":[{"id":"F1","title":"fixed behavior","verify":"go test ./..."}]}`)
	require.NoError(t, os.MkdirAll(filepath.Join(env.workingDir, workplan.Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, workplan.Dir, workplan.PlanFile), plan, 0o600))
	l := &evaluatorLedger{root: env.workingDir, changed: true, lastEdit: 2, redGreen: []workplan.RedEvidence{{Command: "go test ./other", GreenSequence: 3}}}
	decision, stop := a.checkRedGreen(t.Context(), "requirements", l)
	require.True(t, stop)
	require.NotEmpty(t, decision.Unverified)
	l.redGreen[0].Command = "go test ./..."
	decision, stop = a.checkRedGreen(t.Context(), "requirements", l)
	require.False(t, stop)
	require.Empty(t, decision.Unverified)
	require.Equal(t, []string{"F1"}, l.redGreen[0].RequirementIDs)
}

func TestChangedFileSecretScanBlocksWithoutLeakingValue(t *testing.T) {
	t.Parallel()
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{})
	const credential = "AKIAQ3ZKJXNVBWRTPLMD"
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "credentials.txt"), []byte(credential), 0o600))
	l := &evaluatorLedger{root: env.workingDir, files: []string{"credentials.txt"}}
	decision, stop := a.scanChangedSecrets(l)
	require.True(t, stop)
	require.NotEmpty(t, decision.Continue)
	require.NotContains(t, decision.Continue, credential)
}

func TestMutationCheckUsesDisposableCopyThroughToolDispatch(t *testing.T) {
	t.Parallel()
	const source = "package fixture\nfunc Live(x int) bool { return x <= 3 }\n"
	calls := 0
	var dirs []string
	checks := fantasy.NewAgentTool("bash", "fixture", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		calls++
		dirs = append(dirs, p.WorkingDir)
		data, err := os.ReadFile(filepath.Join(p.WorkingDir, "core.go"))
		require.NoError(t, err)
		exit := 0
		output := "ok"
		if calls > 1 && strings.Contains(string(data), "x < 3") {
			exit = 1
			output = "--- FAIL: TestLive"
		}
		meta, _ := json.Marshal(tools.BashResponseMetadata{ExitCode: &exit, Executed: true})
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(output), json.RawMessage(meta)), nil
	})
	a, env, _ := newWorkLoopAgent(t, &scriptedModel{}, checks)
	enabled := true
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{Harness: &config.Harness{Mutation: &enabled}}}).Scoped(env.workingDir)
	sourcePath := filepath.Join(env.workingDir, "core.go")
	testPath := filepath.Join(env.workingDir, "core_test.go")
	require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o600))
	require.NoError(t, os.WriteFile(testPath, []byte("package fixture\n"), 0o600))
	l := &evaluatorLedger{root: env.workingDir, baselines: map[string]fileBaseline{
		sourcePath: {content: "package fixture\n", existed: true},
		testPath:   {existed: false},
	}}
	decision, stop := a.mutationCheck(t.Context(), "mutation-integration", l)
	require.False(t, stop)
	require.Empty(t, decision.Unverified)
	require.Equal(t, 2, calls)
	require.Len(t, dirs, 2)
	require.NotEqual(t, env.workingDir, dirs[0])
	require.Equal(t, dirs[0], dirs[1])
	data, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	require.Equal(t, source, string(data))
}

func TestLintDeltaKeepsOldErrorsSeparate(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"Error new symbol missing"}, diagnosticDelta([]string{"Error old.go:10 undefined old"}, []string{"Error old.go:20 undefined old", "Error new symbol missing"}))
	require.NotEmpty(t, fuzzHint("parse.go", "func Parse() {}"))
	require.Empty(t, fuzzHint("label.go", "const Label = `Hello`"))
	require.Len(t, diagnosticDelta([]string{"Error missing value"}, []string{"Error missing value", "Error missing value"}), 1)
}
