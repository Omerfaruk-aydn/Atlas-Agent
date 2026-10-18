package workplan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckProblemFlagsServersAndProse(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"npm run dev -- --host 0.0.0.0",
		"npm run build; npm run preview",
		"vite",
		"python -m http.server 8000",
		"npm run build; browser desktop/mobile inspection",
		"inspect with browser at 390px",
		"npx playwright test",
		"npm run test:e2e",
		"npx cypress run",
	} {
		require.NotEmpty(t, CheckProblem(command), command)
	}
	for _, command := range []string{
		"npm run build",
		"npm test",
		"go test ./...",
		"npm run build && npx vitest run",
	} {
		require.Empty(t, CheckProblem(command), command)
	}
}

func TestPlanCheckProblemsListsEachCommandOnce(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Verify:   []Check{{Command: "npm run dev"}, {Command: "npm test"}},
		Features: []Feature{{ID: "F1", Verify: "npm run dev"}, {ID: "F2", Verify: "visual check of the hero"}},
	}
	problems := plan.CheckProblems()
	require.Len(t, problems, 2)
	require.Contains(t, problems[0], "`npm run dev`")
	require.Contains(t, problems[1], "manual step")
	require.Nil(t, (*Plan)(nil).CheckProblems())
}
