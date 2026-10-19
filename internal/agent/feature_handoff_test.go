package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

const handoffPlan = `{"goal":"solar system","verify":[{"name":"unit","command":"npm test"}],"features":[
{"id":"F1","title":"Scene","acceptance":"planets render","done":%s},
{"id":"F2","title":"Orbits","acceptance":"planets move along their orbits","done":false}]}`

func writeHandoffProject(t *testing.T, root string, f1Done bool) {
	t.Helper()
	done := "false"
	if f1Done {
		done = "true"
	}
	writeWorkPlan(t, root, strings.Replace(handoffPlan, "%s", done, 1))
}

func writeNotes(t *testing.T, root string, at time.Time) {
	t.Helper()
	path := filepath.Join(root, workplan.Dir, workplan.ProgressFile)
	require.NoError(t, os.WriteFile(path, []byte("F1 done: scene renders.\nNext: F2 orbits."), 0o644))
	require.NoError(t, os.Chtimes(path, at, at))
}

func setPlanTime(t *testing.T, root string, at time.Time) {
	t.Helper()
	path := filepath.Join(root, workplan.Dir, workplan.PlanFile)
	require.NoError(t, os.Chtimes(path, at, at))
}

func testCatwalkModel() catwalk.Model {
	return catwalk.Model{ContextWindow: 200_000, DefaultMaxTokens: 10_000}
}

func messageParams(text string) message.CreateMessageParams {
	return message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

func newTestWatch() *handoffWatch {
	return &handoffWatch{baseline: map[string]bool{}, scratch: &evaluatorLedger{}}
}

// A feature finished in the turn hands off once its checks passed after
// the last edit, the notes are current and the context is large; each
// missing condition keeps the conversation.
func TestFeatureHandoffWaitsForEveryCondition(t *testing.T) {
	t.Parallel()
	now := time.Now()
	passing := steps(editStep("1", "src/scene.ts"), bashStep("2", "npm test", 0))
	cases := []struct {
		name   string
		f1Done bool
		notes  time.Time
		steps  []fantasy.StepResult
		tokens int64
		want   bool
	}{
		{"ready", true, now, passing, 90_000, true},
		{"context still small", true, now, passing, 60_000, false},
		{"no feature finished", false, now, passing, 90_000, false},
		{"notes older than the plan", true, now.Add(-time.Hour), passing, 90_000, false},
		{"check not run", true, now, steps(editStep("1", "src/scene.ts")), 90_000, false},
		{"check failed", true, now, steps(editStep("1", "src/scene.ts"), bashStep("2", "npm test", 1)), 90_000, false},
		{"edit after the check", true, now, steps(bashStep("1", "npm test", 0), editStep("2", "src/scene.ts")), 90_000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeHandoffProject(t, root, tc.f1Done)
			setPlanTime(t, root, now.Add(-time.Minute))
			writeNotes(t, root, tc.notes)
			w := newTestWatch()
			require.Equal(t, tc.want, w.due(root, tc.steps, 200_000, tc.tokens))
			if tc.want {
				require.Equal(t, "F1", w.feature.ID)
			}
		})
	}
}

// A feature that was already done when the turn started, or a plan with
// nothing left to do, never hands off.
func TestFeatureHandoffNeedsANewlyFinishedFeatureAndWorkLeft(t *testing.T) {
	t.Parallel()
	now := time.Now()
	root := t.TempDir()
	writeHandoffProject(t, root, true)
	setPlanTime(t, root, now.Add(-time.Minute))
	writeNotes(t, root, now)
	passing := steps(editStep("1", "src/scene.ts"), bashStep("2", "npm test", 0))

	w := newTestWatch()
	w.baseline["F1"] = true
	require.False(t, w.due(root, passing, 200_000, 90_000))

	writeWorkPlan(t, root, `{"verify":[{"name":"unit","command":"npm test"}],"features":[{"id":"F1","title":"Scene","done":true}]}`)
	setPlanTime(t, root, now.Add(-time.Minute))
	require.False(t, newTestWatch().due(root, passing, 200_000, 90_000))
}

// Steps are replayed on the scratch ledger once each, however often the
// watch is asked, and only once the cheap conditions hold.
func TestFeatureHandoffReplaysEachStepOnce(t *testing.T) {
	t.Parallel()
	now := time.Now()
	root := t.TempDir()
	writeHandoffProject(t, root, true)
	setPlanTime(t, root, now.Add(-time.Minute))
	writeNotes(t, root, now)
	w := newTestWatch()
	turn := append(steps(editStep("1", "src/a.ts")), steps(editStep("2", "src/b.ts"))...)
	require.False(t, w.due(root, turn, 200_000, 1_000), "a small context returns before any replay")
	require.Zero(t, w.seen)
	require.False(t, w.due(root, turn[:1], 200_000, 90_000), "npm test has not run")
	require.False(t, w.due(root, turn, 200_000, 90_000))
	require.False(t, w.due(root, turn, 200_000, 90_000))
	require.Equal(t, []string{"src/a.ts", "src/b.ts"}, w.scratch.files)
	require.Equal(t, 2, w.scratch.seq)
}

func TestHandoffTextCarriesTheRecordAndTheNextFeature(t *testing.T) {
	t.Parallel()
	plan, err := workplan.Parse([]byte(strings.Replace(handoffPlan, "%s", "true", 1)))
	require.NoError(t, err)
	text := handoffText(handoffInput{
		finished: workplan.Feature{ID: "F1", Title: "Scene"},
		plan:     plan,
		progress: "F1 done.\nNext: F2.",
		turns: []engineering.UserTurn{
			{Seq: 1, Kind: "request", Text: "build a solar system\nwith real scale"},
			{Seq: 2, Kind: "continuation", Text: "devam et"},
			{Seq: 3, Kind: "follow_up", Text: "make Saturn's rings visible"},
		},
		changed: []string{"src/scene.ts", "index.html"},
		jobs:    []shell.BackgroundShellInfo{{ID: "007", Command: "npm run dev", StartedAt: time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)}},
	})
	require.Contains(t, text, "Feature F1 (Scene) is done")
	require.Contains(t, text, "1. build a solar system\n   with real scale")
	require.Contains(t, text, "3. make Saturn's rings visible")
	require.NotContains(t, text, "devam et", "a bare continue carries no requirement")
	require.Contains(t, text, "F1 done.\nNext: F2.")
	require.Less(t, strings.Index(text, "- index.html"), strings.Index(text, "- src/scene.ts"))
	require.Contains(t, text, "- 007: npm run dev")
	require.Contains(t, text, "Continue with feature F2: Orbits. Acceptance: planets move along their orbits")
	require.Contains(t, text, "View a file before you edit it")
}

func TestHandoffTextBoundsLongInputs(t *testing.T) {
	t.Parallel()
	plan, err := workplan.Parse([]byte(strings.Replace(handoffPlan, "%s", "true", 1)))
	require.NoError(t, err)
	var files []string
	for i := range handoffMaxFiles + 5 {
		files = append(files, fmt.Sprintf("src/f%02d.ts", i))
	}
	text := handoffText(handoffInput{
		finished: workplan.Feature{ID: "F1"},
		plan:     plan,
		progress: strings.Repeat("old note\n", 2000) + "latest note",
		turns:    []engineering.UserTurn{{Seq: 1, Kind: "request", Text: strings.Repeat("a", handoffMaxTurnText+100)}},
		changed:  files,
	})
	require.Contains(t, text, "[truncated]")
	require.Contains(t, text, "[earlier notes omitted]")
	require.Contains(t, text, "latest note")
	require.Contains(t, text, "- and 5 more")
	require.Less(t, len(text), handoffMaxTurnText+handoffMaxProgress+8<<10)
}

// The handoff becomes the session's summary: the next request starts from
// it as the first message, and the conversation before it is dropped.
func TestHandOffSavesTheHandoffAsTheSummary(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	store := engineering.NewStore(t.TempDir())
	writeHandoffProject(t, env.workingDir, true)
	writeNotes(t, env.workingDir, time.Now())
	a := NewSessionAgent(SessionAgentOptions{
		LargeModel:  Model{CatwalkCfg: testCatwalkModel()},
		SmallModel:  Model{CatwalkCfg: testCatwalkModel()},
		IsYolo:      true,
		Sessions:    env.sessions,
		Messages:    env.messages,
		WorkingDir:  env.workingDir,
		Engineering: store,
	}).(*sessionAgent)

	sess, err := env.sessions.Create(t.Context(), "solar")
	require.NoError(t, err)
	_, err = recordWorkTurn(t.Context(), store, sess.ID, "build a solar system", "")
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), sess.ID, messageParams("build a solar system"))
	require.NoError(t, err)

	require.NoError(t, a.handOff(t.Context(), sess.ID, workplan.Feature{ID: "F1", Title: "Scene"}, []string{"src/scene.ts"}))

	saved, err := env.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, saved.SummaryMessageID)
	require.Zero(t, saved.PromptTokens)
	msgs, err := a.getSessionMessages(t.Context(), saved)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0].Content().Text, "1. build a solar system")
	require.Contains(t, msgs[0].Content().Text, "- src/scene.ts")
	require.Contains(t, msgs[0].Content().Text, "Continue with feature F2")
}

