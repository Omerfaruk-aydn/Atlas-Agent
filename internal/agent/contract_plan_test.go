package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func changedFiles(n int) *evaluatorLedger {
	l := &evaluatorLedger{}
	for i := 0; i < n; i++ {
		l.files = append(l.files, fmt.Sprintf("src/f%d.go", i))
	}
	return l
}

func TestPlanlessRequestIsAskedForAPlanOnce(t *testing.T) {
	t.Parallel()
	a := &sessionAgent{}
	root := t.TempDir()

	require.Empty(t, a.planlessPrompt(root, changedFiles(3)), "a small change needs no plan")

	ledger := changedFiles(config.DefaultAutoPlanFiles)
	prompt := a.planlessPrompt(root, ledger)
	require.Contains(t, prompt, "no work plan")
	require.Contains(t, prompt, ".atlas/work.json")
	require.Empty(t, a.planlessPrompt(root, ledger), "it is asked only once")
}

func TestEditingOneFileManyTimesIsNotALargeChange(t *testing.T) {
	t.Parallel()
	ledger := &evaluatorLedger{}
	for i := 0; i < 20; i++ {
		ledger.files = append(ledger.files, "main.go")
	}
	require.Empty(t, (&sessionAgent{}).planlessPrompt(t.TempDir(), ledger))
}

func TestAnExistingPlanFileIsNeverAskedForAgain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".atlas"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".atlas", "work.json"), []byte("{broken"), 0o644))
	require.Empty(t, (&sessionAgent{}).planlessPrompt(root, changedFiles(10)))
}

func TestAutoPlanThresholdCanBeTunedOrSwitchedOff(t *testing.T) {
	t.Parallel()
	var none *config.WorkPlan
	require.Equal(t, config.DefaultAutoPlanFiles, none.AutoPlanThreshold())
	require.Equal(t, 3, (&config.WorkPlan{AutoPlanFiles: 3}).AutoPlanThreshold())
	require.Zero(t, (&config.WorkPlan{AutoPlanFiles: -1}).AutoPlanThreshold())
}
