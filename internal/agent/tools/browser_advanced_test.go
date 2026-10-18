package tools

import (
	"context"
	"encoding/base32"
	"path/filepath"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/stretchr/testify/require"
)

type visibleFakeBrowserSessions struct{ *fakeBrowserSessions }

func (*visibleFakeBrowserSessions) UsesDesktop() bool { return true }

func TestVisibleBrowserWaitsForSharedDesktopOwnership(t *testing.T) {
	release, err := interaction.Default.Acquire(t.Context(), "desktop-fixture-owner", "desktop")
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), SessionIDContextKey, "visible-browser-fixture"), 20*time.Millisecond)
	defer cancel()
	tool := newBrowserTool(&mockBashPermissionService{}, t.TempDir(), &visibleFakeBrowserSessions{newFakeBrowserSessions()}, "fixture")
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "visible-fixture", Input: `{"action":"navigate","url":"https://fixture.test"}`})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "deadline")
}

func TestTOTPStandardVectorsAndSecretIsolation(t *testing.T) {
	seed := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		timestamp int64
		expected  string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		code, err := totp(seed, time.Unix(v.timestamp, 0))
		require.NoError(t, err)
		require.Equal(t, v.expected, code)
	}
	t.Setenv("ATLAS_TOTP_FIXTURE", seed)
	session := &fakeBrowserSession{}
	response, err := runBrowserAuth(t.Context(), session, BrowserParams{SecretEnv: "ATLAS_TOTP_FIXTURE", Selector: "#otp"})
	require.NoError(t, err)
	require.False(t, response.IsError)
	require.Len(t, session.typedText, 6)
	require.NotContains(t, response.Content, session.typedText)
	require.NotContains(t, response.Content, seed)
	response, err = runBrowserAuth(t.Context(), session, BrowserParams{SecretEnv: "UNRELATED_SECRET", Selector: "#otp"})
	require.NoError(t, err)
	require.True(t, response.IsError)
}

func TestInteractionPathRejectsTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := interactionPath(root, "../outside")
	require.Error(t, err)
	_, err = interactionPath(root, filepath.Join(t.TempDir(), "outside"))
	require.Error(t, err)
	_, err = interactionPath(root, "fixture.png")
	require.NoError(t, err)
	require.Equal(t, "https://example.test/", safeTraceURL("https://user:password@example.test/?token=secret#x"))
}
