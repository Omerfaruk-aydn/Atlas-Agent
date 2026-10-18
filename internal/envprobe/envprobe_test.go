package envprobe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionParsing(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"go version go1.26.6 windows/amd64":         "1.26.6",
		"v22.3.0":                                   "22.3.0",
		"Python 3.12.1":                             "3.12.1",
		`openjdk version "21.0.2" 2024-01-16`:       "21.0.2",
		"Docker version 27.1.1, build 6312585":      "27.1.1",
		"rustc 1.80.0 (051478957 2024-07-21)":       "1.80.0",
		"git version 2.45.2.windows.1":              "2.45.2.windows.1",
		"GNU Make 4.4.1\nBuilt for x86_64-pc-linux": "4.4.1",
	}
	for output, want := range cases {
		require.Equal(t, want, versionPattern.FindString(firstLine(output)), output)
	}
	require.Empty(t, firstLine("\n \n"))
}

func TestProbesHaveUniqueNames(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, p := range probes {
		require.False(t, seen[p.name], p.name)
		require.NotEmpty(t, p.commands)
		seen[p.name] = true
	}
}
