package agent

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/memory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

// gateAgent is a session agent with just what the contract gate uses.
func gateAgent(t *testing.T, root string) *sessionAgent {
	t.Helper()
	return &sessionAgent{
		workingDir:      root,
		tools:           csync.NewSliceFrom[fantasy.AgentTool](nil),
		evaluatorModel:  csync.NewValue(ptrBox[Model]{}),
		advisorModel:    csync.NewValue(ptrBox[Model]{}),
		harness:         harnessmetrics.New(func() string { return filepath.Join(root, ".atlas") }),
		evaluatorTools:  csync.NewSliceFrom[fantasy.AgentTool](nil),
		evaluatorRounds: csync.NewValue(0),
	}
}

func gitInit(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		require.NoError(t, cmd.Run())
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, workplan.Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, workplan.Dir, ".gitignore"), []byte("*\n"), 0o644))
	return root
}

func failedGap(command string) contractGap {
	return contractGap{req: workplan.Requirement{Command: command, Reason: "contract check"}, kind: gapFailed, tail: "FAIL"}
}

func TestContractEscalatesRepeatedFailures(t *testing.T) {
	t.Parallel()

	a := gateAgent(t, t.TempDir())
	ledger := &evaluatorLedger{}
	gaps := []contractGap{failedGap("make test")}

	first := a.sendContractBack(t.Context(), "s", ledger, gaps, "").Continue
	require.NotContains(t, first, "step back")
	require.NotContains(t, first, "different approaches")

	second := a.sendContractBack(t.Context(), "s", ledger, gaps, "").Continue
	require.Contains(t, second, "Stop patching and step back")
	require.Contains(t, second, "hypotheses")

	third := a.sendContractBack(t.Context(), "s", ledger, gaps, "").Continue
	require.Contains(t, third, "Try two genuinely different approaches")

	fourth := a.sendContractBack(t.Context(), "s", ledger, gaps, "")
	require.Contains(t, fourth.Continue, "check 4 of 4")
	final := a.sendContractBack(t.Context(), "s", ledger, gaps, "")
	require.Empty(t, final.Continue)
	require.Contains(t, final.Unresolved, "Verification contract not met")

	events, err := harnessmetrics.Load(filepath.Join(a.workingDir, ".atlas"), timeZero())
	require.NoError(t, err)
	kinds := map[harnessmetrics.Kind]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	require.Equal(t, 4, kinds[harnessmetrics.ContractSentBack])
	require.Equal(t, 1, kinds[harnessmetrics.StepBack])
	require.GreaterOrEqual(t, kinds[harnessmetrics.Alternatives], 1)
	require.Equal(t, 1, kinds[harnessmetrics.ContractUnresolved])
}

func TestContractStreakResetsWhenACheckStopsFailing(t *testing.T) {
	t.Parallel()

	a := gateAgent(t, t.TempDir())
	ledger := &evaluatorLedger{}
	a.sendContractBack(t.Context(), "s", ledger, []contractGap{failedGap("make test")}, "")
	notRun := contractGap{req: workplan.Requirement{Command: "make test", Reason: "r"}, kind: gapNotRun}
	a.sendContractBack(t.Context(), "s", ledger, []contractGap{notRun}, "")
	again := a.sendContractBack(t.Context(), "s", ledger, []contractGap{failedGap("make test")}, "").Continue
	require.NotContains(t, again, "step back", "a streak counts consecutive failures only")
}

func TestRegressionNoteNamesTheFilesChangedSinceTheLastGreen(t *testing.T) {
	t.Parallel()

	root := gitInit(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "cart.go"), []byte("package cart\n"), 0o644))
	_, err := workplan.RecordGreen(context.Background(), root, nil, []string{"go test ./..."})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "cart.go"), []byte("package cart\n// broken\n"), 0o644))

	a := gateAgent(t, root)
	text := a.sendContractBack(t.Context(), "s", &evaluatorLedger{}, []contractGap{failedGap("go test ./...")}, "").Continue
	require.Contains(t, text, "Regression: this check passed at checkpoint")
	require.Contains(t, text, "cart.go")
	require.Contains(t, text, "git diff")
}

func TestProbeCatchesACheckThatCannotFail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := gitInit(t)
	writeWorkPlan(t, root, `{"verify":[{"command":"make all"}],"features":[{"id":"F1","title":"Totals","verify":"make totals"}]}`)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("before F1"), 0o644))
	_, err := workplan.RecordGreen(ctx, root, nil, []string{"make all"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("with F1"), 0o644))
	writeWorkPlan(t, root, `{"verify":[{"command":"make all"}],"features":[{"id":"F1","title":"Totals","verify":"make totals","done":true}]}`)

	a := gateAgent(t, root)
	ledger := &evaluatorLedger{changed: true}
	ledger.record(steps(editStep("e", "a.txt"), bashStep("1", "make all", 0), bashStep("2", "make totals", 0)))

	decision, stop := a.checkContract(ctx, "s", ledger)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "Prove the check for F1 can fail")
	require.Contains(t, decision.Continue, "Try to break what was just finished", "the breaker pass comes with it")
	probe, ok := ledger.probes["F1"]
	require.True(t, ok)
	data, err := os.ReadFile(filepath.Join(probe.dir, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "before F1", string(data), "the probe holds the code from before the feature")

	// The agent runs the check in the probe and it passes: the check is
	// vacuous.
	rel, err := filepath.Rel(root, probe.dir)
	require.NoError(t, err)
	ledger.record(steps(bashDirStep("3", "make totals", filepath.ToSlash(rel), 0), bashStep("4", "make all", 0)))
	decision, stop = a.checkContract(ctx, "s", ledger)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "does not test F1")
	_, err = os.Stat(probe.dir)
	require.True(t, os.IsNotExist(err), "the probe is removed once judged")
	require.NotContains(t, decision.Continue, "Try to break", "each feature is followed up once")
}

func TestProbeThatFailsIsAccepted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := gitInit(t)
	a := gateAgent(t, root)
	dir := filepath.Join(root, workplan.Dir, workplan.ProbeDir, "F1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	ledger := &evaluatorLedger{probes: map[string]contractProbe{"F1": {feature: "F1", command: "make totals", dir: dir}}}
	ledger.record(steps(bashDirStep("1", "make totals", dir, 2)))
	require.Empty(t, a.settleProbes(ctx, "s", ledger))
	require.Empty(t, ledger.probes)
}

func TestBreakerPassWithoutGitFollowsUpEachFeatureOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeWorkPlan(t, root, `{"verify":[{"command":"make all"}],"features":[{"id":"F1","title":"Login","done":true},{"id":"F2","title":"Export"}]}`)
	a := gateAgent(t, root)
	ledger := &evaluatorLedger{changed: true}
	ledger.record(steps(editStep("e", "a.go"), bashStep("1", "make all", 0)))

	decision, stop := a.checkContract(t.Context(), "s", ledger)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "F1 (Login)")
	require.NotContains(t, decision.Continue, "F2", "open features are not attacked yet")
	require.Contains(t, decision.Continue, "Write a failing test")

	ledger.changed = true
	decision, stop = a.checkContract(t.Context(), "s", ledger)
	require.False(t, stop, "nothing new to follow up")
	require.Empty(t, decision.Continue)
}

func TestBreakerDelegatesWhenTheAgentToolExists(t *testing.T) {
	t.Parallel()

	note := breakerNote([]workplan.Feature{{ID: "F1", Title: "Login"}}, true)
	require.Contains(t, note, "Delegate this to a sub-agent")
	require.NotContains(t, breakerNote([]workplan.Feature{{ID: "F1", Title: "Login"}}, false), "Delegate")
}

func TestWorkPlanOptionsCanTurnFollowUpsOff(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeWorkPlan(t, root, `{"verify":[{"command":"make all"}],"features":[{"id":"F1","title":"Login","done":true}]}`)
	off := false
	a := gateAgent(t, root)
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{
		DataDirectory: filepath.Join(root, ".atlas"),
		WorkPlan:      &config.WorkPlan{Breaker: &off, PlanReview: &off, LearnRecipes: &off},
	}})
	ledger := &evaluatorLedger{changed: true}
	ledger.record(steps(editStep("e", "a.go"), bashStep("1", "make all", 0)))
	_, stop := a.checkContract(t.Context(), "s", ledger)
	require.False(t, stop)
}

func TestPlanReviewRunsOncePerPlanVersion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeWorkPlan(t, root, `{"goal":"shop","verify":[{"command":"ls"}],"features":[{"id":"F1","title":"Cart"}]}`)
	reviewer := &scriptedModel{repeat: &scriptedStep{text: "VERDICT: FAIL\n- `ls` cannot fail; it does not test the cart"}}
	a := gateAgent(t, root)
	withEvaluator(a, reviewer, 2)
	a.engineering = nil
	ledger := &evaluatorLedger{changed: true, request: "build a shop"}

	decision, stop := a.checkContract(t.Context(), "s", ledger)
	require.True(t, stop)
	require.Contains(t, decision.Continue, "`ls` cannot fail")
	require.Contains(t, decision.Continue, "Revise .atlas/work.json")
	require.Len(t, reviewer.calls(), 1)
	require.Contains(t, requestText(reviewer.calls()[0]), "build a shop")

	ledger.changed = true
	_, _ = a.checkContract(t.Context(), "s", ledger)
	require.Len(t, reviewer.calls(), 1, "the same plan is not reviewed twice")

	// Marking a feature done is progress, not a new plan.
	writeWorkPlan(t, root, `{"goal":"shop","verify":[{"command":"ls"}],"features":[{"id":"F1","title":"Cart","done":true}]}`)
	fresh := gateAgent(t, root)
	withEvaluator(fresh, reviewer, 2)
	_, _ = fresh.checkContract(t.Context(), "s", &evaluatorLedger{changed: true})
	require.Len(t, reviewer.calls(), 1, "the review survives in the plan state")

	writeWorkPlan(t, root, `{"goal":"shop","verify":[{"command":"npm test"}],"features":[{"id":"F1","title":"Cart"}]}`)
	_, _ = fresh.checkContract(t.Context(), "s", &evaluatorLedger{changed: true})
	require.Len(t, reviewer.calls(), 2, "a changed contract is reviewed again")
	followUp := requestText(reviewer.calls()[1])
	require.Contains(t, followUp, "follow-up review")
	require.Contains(t, followUp, "`ls` cannot fail", "the follow-up checks the earlier findings")

	// Each revision for the findings is a new version; the reviews stop
	// after the follow-up instead of looping.
	writeWorkPlan(t, root, `{"goal":"shop","verify":[{"command":"npm test -- cart"}],"features":[{"id":"F1","title":"Cart"}]}`)
	decision, _ = fresh.checkContract(t.Context(), "s", &evaluatorLedger{changed: true})
	require.Len(t, reviewer.calls(), 2, "at most two reviews of one plan")
	require.NotContains(t, decision.Continue, "fresh context")

	// Another goal is new work and is reviewed afresh.
	writeWorkPlan(t, root, `{"goal":"blog","verify":[{"command":"npm test"}],"features":[{"id":"F1","title":"Posts"}]}`)
	_, _ = fresh.checkContract(t.Context(), "s", &evaluatorLedger{changed: true})
	require.Len(t, reviewer.calls(), 3)
	require.NotContains(t, requestText(reviewer.calls()[2]), "follow-up review")
}

func TestAtlasNotesDoNotMakeChecksStale(t *testing.T) {
	t.Parallel()

	ledger := &evaluatorLedger{}
	ledger.record(steps(editStep("e", "src/app.ts"), bashStep("1", "npm test", 0)))
	ledger.record(steps(editStep("n", ".atlas/night-probe.mjs"), editStep("p", "C:/proj/.atlas/progress.md")))
	require.Empty(t, ledger.contractGaps([]workplan.Requirement{{Command: "npm test"}}))
	ledger.record(steps(editStep("s", "src/app.ts")))
	require.Len(t, ledger.contractGaps([]workplan.Requirement{{Command: "npm test"}}), 1)
}

func TestLearnRecipesSavesVerifiedChecksOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a := gateAgent(t, root)
	a.taskContextConfig = config.NewTestStore(&config.Config{Options: &config.Options{DataDirectory: filepath.Join(root, ".atlas")}})
	reqs := []workplan.Requirement{{Command: "go test ./..."}, {Command: "npm run e2e"}}
	a.learnRecipes("s", reqs)
	a.learnRecipes("s", reqs)

	content, err := memoryStore(a.taskContextConfig).Read(memory.ScopeProject)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(content, "`go test ./...`"), "saved once")
	require.Contains(t, content, "`npm run e2e`")
}

func TestVisualReviewSendsScreenshotsWithTheRubric(t *testing.T) {
	t.Parallel()

	reviewer := &scriptedModel{repeat: &scriptedStep{text: "VERDICT: PASS"}}
	a := gateAgent(t, t.TempDir())
	withEvaluator(a, reviewer, 2)
	m := a.evaluatorModel.Get().v
	m.CatwalkCfg.SupportsImages = true
	image := fantasy.FilePart{Filename: "screenshot", Data: []byte{1, 2, 3}, MediaType: "image/png"}
	_, err := a.runEvaluator(t.Context(), "s", "review", []fantasy.FilePart{image})
	require.NoError(t, err)
	calls := reviewer.calls()
	require.Len(t, calls, 1)
	text := requestText(calls[0])
	require.Contains(t, text, "Originality (weighs most)")
	require.Contains(t, text, "attached image(s)")
	var files int
	for _, msg := range calls[0].Prompt {
		for _, part := range msg.Content {
			if _, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
				files++
			}
		}
	}
	require.Equal(t, 1, files)
}

func TestLedgerKeepsTheLatestScreenshots(t *testing.T) {
	t.Parallel()

	var l evaluatorLedger
	for i := range evaluatorMaxImages + 2 {
		l.keepImage(fantasy.ToolResultOutputContentMedia{Data: b64(gradientPNG(t, 20+i, 20)), MediaType: "image/png"})
	}
	l.keepImage(fantasy.ToolResultOutputContentText{Text: "not an image"})
	require.Len(t, l.images, evaluatorMaxImages)
	w, _ := imageSize(t, l.images[len(l.images)-1].Data)
	require.Equal(t, 20+evaluatorMaxImages+1, w, "the newest is kept last")
}

func bashDirStep(id, command, dir string, exit int) []fantasy.Content {
	parts := bashStep(id, command, exit)
	call := parts[0].(fantasy.ToolCallContent)
	call.Input = `{"command":` + jsonString(command) + `,"working_dir":` + jsonString(dir) + `}`
	parts[0] = call
	return parts
}

func timeZero() time.Time { return time.Time{} }

func b64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }
