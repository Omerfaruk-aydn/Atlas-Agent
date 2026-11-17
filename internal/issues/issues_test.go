package issues

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLedgerLifecycle(t *testing.T) {
	t.Parallel()
	l := New()
	a, err := l.Add("nil pointer in parser", Critical, "parse.go:10")
	require.NoError(t, err)
	b, err := l.Add("typo in comment", Minor, "")
	require.NoError(t, err)
	again, err := l.Add("Nil pointer in parser", Critical, "parse.go:10")
	require.NoError(t, err)
	require.Equal(t, a, again, "the same open issue is not duplicated")

	require.Len(t, l.OpenAtLeast(Major), 1)
	require.Len(t, l.OpenAtLeast(Minor), 2)

	require.ErrorContains(t, l.Close(a, "", ""), "command")
	require.ErrorContains(t, l.Close(a, "go test ./...", "ok"), "output")
	require.ErrorContains(t, l.Close(99, "go test", "ok  	pkg	0.1s"), "no issue")
	require.NoError(t, l.Close(a, "go test ./parse", "ok  	pkg/parse	0.2s"))
	require.ErrorContains(t, l.Close(a, "go test ./parse", "ok  	pkg/parse	0.2s"), "already closed")
	require.Empty(t, l.OpenAtLeast(Major))

	all := l.All()
	require.Equal(t, b, all[0].ID, "open issues sort first")
	require.Contains(t, Summary(all), "closed by `go test ./parse`")
	require.Equal(t, "No issues recorded.", Summary(nil))
}

func TestValidation(t *testing.T) {
	t.Parallel()
	l := New()
	_, err := l.Add(" ", Major, "")
	require.Error(t, err)
	_, err = l.Add("x", 0, "")
	require.Error(t, err)
	for in, want := range map[string]Severity{"minor": Minor, "HIGH": Major, "blocker": Critical} {
		got, err := ParseSeverity(in)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err = ParseSeverity("meh")
	require.Error(t, err)
}
