package update

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckForUpdate_Old(t *testing.T) {
	info, err := Check(t.Context(), "v0.10.0", testClient{"v0.11.0"})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.True(t, info.Available())
}

func TestDevelopmentBuildClassification(t *testing.T) {
	t.Parallel()
	for _, version := range []string{
		"devel", "unknown", "v0.16.1+dirty",
		"v0.16.1-support-idle-fix-dev", "v0.16.1-support-auto-dev",
		"v0.16.1-dev.browseruse.a2f7d70.dirty", "v0.16.1+dev",
		"v0.16.1-0.20261011210000-06c807842604",
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			info := Info{Current: version, Latest: "0.17.0"}
			require.True(t, info.IsDevelopment())
			require.False(t, info.Available())
			// A nil client proves no network/update client is consulted.
			checked, err := Check(t.Context(), version, nil)
			require.NoError(t, err)
			require.True(t, checked.IsDevelopment())
			require.False(t, checked.Available())
		})
	}
	for _, version := range []string{"v0.16.1", "v0.16.1-beta.1", "v0.16.1-rc.1", "v0.16.1+build.1", "v0.16.1-device-preview"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			require.False(t, Info{Current: version}.IsDevelopment())
		})
	}
}

func TestCheckForUpdate_Beta(t *testing.T) {
	t.Run("current is stable", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.10.0", testClient{"v0.11.0-beta.1"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.False(t, info.Available())
	})

	t.Run("current is also beta", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", testClient{"v0.11.0-beta.2"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})

	t.Run("current is beta, latest isn't", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", testClient{"v0.11.0"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})
}

type testClient struct{ tag string }

// Latest implements Client.
func (t testClient) Latest(ctx context.Context) (*Release, error) {
	return &Release{
		TagName: t.tag,
		HTMLURL: "https://example.org",
	}, nil
}
