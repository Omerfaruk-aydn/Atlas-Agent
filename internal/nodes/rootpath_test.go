package nodes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchRootSeparatesDriveLabelFromDrivePath(t *testing.T) {
	t.Parallel()
	roots := []Root{{Label: "C", Path: `C:\`}, {Label: "Work", Path: `D:\Work`}}
	cases := []struct {
		in, label, rest string
		denied          bool
	}{
		{in: `C:\Users\ersen\Downloads\a.png`, label: "C", rest: "Users/ersen/Downloads/a.png"},
		{in: `C:/Users/ersen/a.png`, label: "C", rest: "Users/ersen/a.png"},
		{in: `c:/users/a.png`, label: "C", rest: "users/a.png"},
		{in: `C/Users/a.png`, label: "C", rest: "Users/a.png"},
		{in: `C:\`, label: "C", rest: "."},
		{in: `C:/`, label: "C", rest: "."},
		{in: `D:\Work\in\r.txt`, label: "Work", rest: "in/r.txt"},
		{in: `D:\Work`, label: "Work", rest: "."},
		{in: `Work/in/r.txt`, label: "Work", rest: "in/r.txt"},
		{in: `/Work/in/r.txt`, label: "Work", rest: "in/r.txt"},
		// Directory boundary, not a string prefix.
		{in: `D:\Work2\x.txt`, denied: true},
		{in: `D:\Other\x.txt`, denied: true},
		// Drive-relative paths are not C:\.
		{in: `C:foo`, denied: true},
		{in: `C:`, denied: true},
		{in: `E:\x`, denied: true},
		{in: `Nope/x`, denied: true},
	}
	for _, tc := range cases {
		r, rest, err := matchRoot(roots, tc.in)
		if tc.denied {
			require.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.label, r.Label, tc.in)
		require.Equal(t, tc.rest, rest, tc.in)
	}
}

func TestMatchRootPrefersTheDeepestRoot(t *testing.T) {
	t.Parallel()
	roots := []Root{{Label: "C", Path: `C:\`}, {Label: "Desktop", Path: `C:\Users\u\Desktop`}}
	r, rest, err := matchRoot(roots, `C:\Users\u\Desktop\a.txt`)
	require.NoError(t, err)
	require.Equal(t, "Desktop", r.Label)
	require.Equal(t, "a.txt", rest)
}

func TestMatchRootPOSIX(t *testing.T) {
	t.Parallel()
	roots := []Root{{Label: "System", Path: "/"}, {Label: "Home", Path: "/home/u"}}
	r, rest, err := matchRoot(roots, "/home/u/x.txt")
	require.NoError(t, err)
	require.Equal(t, "Home", r.Label)
	require.Equal(t, "x.txt", rest)
	r, rest, err = matchRoot(roots, "/etc/hosts")
	require.NoError(t, err)
	require.Equal(t, "System", r.Label)
	require.Equal(t, "etc/hosts", rest)
	_, _, err = matchRoot(roots, `C:\x`)
	require.Error(t, err)
}

func TestDeniedPathNamesRealFolders(t *testing.T) {
	t.Parallel()
	_, _, err := matchRoot([]Root{{Label: "C", Path: `C:\`}}, "Z/x")
	require.ErrorContains(t, err, `C (C:\)`)
}
