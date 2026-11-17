package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchesIf(t *testing.T) {
	t.Parallel()
	push := `{"command":"git push origin main"}`
	require.True(t, matchesIf("", "bash", push))
	require.True(t, matchesIf("bash", "bash", push))
	require.True(t, matchesIf("Bash(git push*)", "bash", push))
	require.True(t, matchesIf("bash(git * main)", "bash", push))
	require.False(t, matchesIf("bash(git pull*)", "bash", push))
	require.False(t, matchesIf("edit(git push*)", "bash", push))
	require.True(t, matchesIf("edit(src/*.go)", "edit", `{"file_path":"src/a.go"}`))
	require.False(t, matchesIf("edit(src/*.go)", "edit", `{"file_path":"docs/a.md"}`))
	require.True(t, matchesIf("*(*secret*)", "view", `{"path":"/x/secret.txt"}`))
	require.False(t, matchesIf("bash(ls)", "bash", `not json`))
	require.False(t, matchesIf("bash(x)", "", ""))
}
