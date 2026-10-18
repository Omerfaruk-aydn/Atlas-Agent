package workplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writePlan(t *testing.T, root, plan, progress string) {
	t.Helper()
	dir := filepath.Join(root, Dir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if plan != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, PlanFile), []byte(plan), 0o644))
	}
	if progress != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ProgressFile), []byte(progress), 0o644))
	}
}

func TestLoadWithoutPlanIsNotAContract(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, "", "notes only")
	plan, progress, err := Load(root)
	require.NoError(t, err)
	require.Nil(t, plan)
	require.Empty(t, progress)
	require.Empty(t, PromptBlock(root))
}

func TestRequirementsCoverContractAndFinishedFeatures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, `{
		"goal": "Bootable kernel with a shell",
		"verify": [{"name": "unit", "command": "  make   test "}],
		"features": [
			{"id": "F1", "title": "Boots", "verify": "make boot-test", "done": true},
			{"id": "F2", "title": "Timer interrupt", "verify": "make timer-test"},
			{"id": "F3", "title": "Shell", "acceptance": "prompt echoes input", "verify": "make test"}
		]
	}`, "")
	plan, _, err := Load(root)
	require.NoError(t, err)

	got := plan.Requirements()
	require.Len(t, got, 2, "open features do not require their check yet")
	require.Equal(t, "make test", got[0].Command, "commands are normalized")
	require.Equal(t, "make boot-test", got[1].Command)
	require.Contains(t, got[1].Reason, "F1")
	require.Len(t, plan.Open(), 2)
}

func TestLoadRejectsMalformedPlans(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"invalid json":     `{"verify": [`,
		"empty command":    `{"verify": [{"command": "  "}]}`,
		"missing id":       `{"features": [{"title": "x"}]}`,
		"duplicate id":     `{"features": [{"id": "A", "title": "x"}, {"id": "A", "title": "y"}]}`,
		"command too long": `{"verify": [{"command": "` + strings.Repeat("x", maxCommandLen+1) + `"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writePlan(t, root, body, "")
			_, _, err := Load(root)
			require.Error(t, err)
			block := PromptBlock(root)
			require.Contains(t, block, `status="unreadable"`, "a broken contract is reported, not hidden")
		})
	}
}

func TestLoadRejectsOversizedPlan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, `{"goal":"`+strings.Repeat("x", maxPlanBytes)+`"}`, "")
	_, _, err := Load(root)
	require.ErrorContains(t, err, "larger than")
}

func TestPromptBlockSummarizesPlanAndProgress(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var features []string
	for i := range maxShownPending + 3 {
		features = append(features, `{"id": "F`+string(rune('a'+i))+`", "title": "feature"}`)
	}
	progress := strings.Repeat("old line\n", 400) + "Next: wire the timer"
	writePlan(t, root, `{"goal": "Ship it", "verify": [{"command": "go test ./..."}], "features": [`+strings.Join(features, ",")+`]}`, progress)

	block := PromptBlock(root)
	require.Contains(t, block, "Goal: Ship it")
	require.Contains(t, block, "Features: 0 of 11 done.")
	require.Contains(t, block, "… 3 more in the file")
	require.Contains(t, block, "- go test ./...")
	require.Contains(t, block, "Next: wire the timer", "the newest notes are kept")
	require.Less(t, len(block), 3000)
}

func TestPromptBlockAsksForAContractWhenThereIsNone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePlan(t, root, `{"goal": "x", "features": [{"id": "A", "title": "a"}]}`, "")
	require.Contains(t, PromptBlock(root), "no verify commands yet")
}

func TestSatisfies(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		ran, required string
		want          bool
	}{
		{"go test ./...", "go test ./...", true},
		{"cd app &&  go   test ./...", "go test ./...", true},
		{"CI=1 npm test 2>&1 | tail -20", "npm test", true},
		{"go test ./... || true", "go test ./...", false},
		{"go test ./...; exit 0", "go test ./...", false},
		{"! go test ./...", "go test ./...", false},
		{"go vet ./...", "go test ./...", false},
		{"anything", "", false},
	} {
		require.Equal(t, tc.want, Satisfies(tc.ran, tc.required), "%q vs %q", tc.ran, tc.required)
	}
}

func TestIsPlanFile(t *testing.T) {
	t.Parallel()

	require.True(t, IsPlanFile(".atlas/work.json"))
	require.True(t, IsPlanFile(filepath.Join("C:", "proj", ".atlas", "progress.md")))
	require.True(t, IsPlanFile("/home/u/p/.atlas/work.json"))
	require.False(t, IsPlanFile("src/work.json"))
	require.False(t, IsPlanFile(".atlas/other.json"))
}
