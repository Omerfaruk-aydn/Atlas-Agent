package webui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileMatchScoreRanksNameAboveDirectory(t *testing.T) {
	t.Parallel()
	exact := fileMatchScore("internal/app/app.go", "app.go")
	prefix := fileMatchScore("internal/app/application.go", "app")
	inside := fileMatchScore("internal/webapp.go", "app")
	dir := fileMatchScore("internal/app/run.go", "app")
	scattered := fileMatchScore("a/big/project/plan.go", "apg")
	require.Greater(t, exact, prefix)
	require.Greater(t, prefix, inside)
	require.Greater(t, inside, dir)
	require.Greater(t, dir, scattered)
	require.Positive(t, scattered)
}

func TestFileMatchScoreRejectsLettersOutOfOrder(t *testing.T) {
	t.Parallel()
	require.Zero(t, fileMatchScore("main.go", "ogm"))
	require.Zero(t, fileMatchScore("main.go", "xyz"))
}

func TestSearchFiles(t *testing.T) {
	t.Parallel()
	files := []string{"docs/readme.md", "src/app.tsx", "src/lib/apply.ts", "go.mod"}

	require.Equal(t, []string{"docs/readme.md", "src/app.tsx"}, searchFiles(files, "", 2), "an empty query keeps the given order")
	require.Equal(t, []string{"src/app.tsx", "src/lib/apply.ts"}, searchFiles(files, "APP", 10), "matching ignores case")
	require.Equal(t, []string{"src/app.tsx"}, searchFiles(files, "app", 1))
	require.Empty(t, searchFiles(files, "nothing-here", 10))
	require.Empty(t, searchFiles(files, "app", -1))
	require.Empty(t, searchFiles(files, "", -1))
}
