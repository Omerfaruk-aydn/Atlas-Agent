package environment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentVersionRequirements(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		version, constraint string
		matches             bool
	}{
		{"1.26.6", ">=1.26.6", true},
		{"1.26.5", ">=1.26.6", false},
		{"20.9.1", ">=18 <21", true},
		{"22.1.0", ">=18 <21", false},
		{"20.9.1", "20.x", true},
		{"21.0.0", "20.x", false},
		{"0.2.9", "^0.2.3", true},
		{"0.3.0", "^0.2.3", false},
		{"1.4.9", "~1.4.2", true},
		{"1.5.0", "~1.4.2", false},
		{"3.12.4", ">=3.10,<3.14", true},
		{"3.14.0", ">=3.10,<3.14", false},
	} {
		matches, err := MatchesVersion(test.version, test.constraint)
		require.NoError(t, err)
		require.Equal(t, test.matches, matches, test.constraint)
	}
	_, err := ObservedVersion(ToolRequirement{Name: "go", Constraint: ">=1.26.6"}, "")
	require.Error(t, err)
	_, err = MatchesVersion("20.1.0-alpha.1", ">=20")
	require.Error(t, err)
	_, err = MatchesVersion("20.1.0", "unsupported range")
	require.Error(t, err)
}
