package agent

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func newSessionModeCoordinator(t *testing.T, mode string) *coordinator {
	t.Helper()
	env := testEnv(t)
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	require.NotNil(t, cfg.Config().Options, "config.Init must populate Options")
	cfg.Config().Options.SessionMode = mode
	return &coordinator{cfg: cfg}
}

func TestSessionModeIsOffByDefault(t *testing.T) {
	c := newSessionModeCoordinator(t, "")

	_, ok := c.sessionMode()
	require.False(t, ok)
	require.Equal(t, "base prompt", c.withSessionMode("base prompt"))
}

func TestSessionModeResolvesABuiltinMode(t *testing.T) {
	c := newSessionModeCoordinator(t, "review")

	mode, ok := c.sessionMode()
	require.True(t, ok)
	require.Equal(t, "review", mode.Name)
	require.True(t, mode.Builtin)
}

func TestSessionModeIsCaseInsensitiveAndTrimmed(t *testing.T) {
	c := newSessionModeCoordinator(t, "  Security  ")

	mode, ok := c.sessionMode()
	require.True(t, ok)
	require.Equal(t, "security", mode.Name)
}

func TestWithSessionModeAppendsTheModeBlock(t *testing.T) {
	c := newSessionModeCoordinator(t, "security")

	got := c.withSessionMode("base prompt")
	require.True(t, strings.HasPrefix(got, "base prompt"), "the mode is appended, never replacing the coder prompt")
	require.Contains(t, got, `<mode name="security">`)
	require.Contains(t, got, "</mode>")
	require.Contains(t, got, "You are a security reviewer.")
}

// A name left behind in the config after a mode was renamed or its file
// deleted must degrade to the ordinary prompt, not break every session
// until someone notices.
func TestUnknownSessionModeFallsBackToTheOrdinaryPrompt(t *testing.T) {
	c := newSessionModeCoordinator(t, "no-such-mode")

	_, ok := c.sessionMode()
	require.False(t, ok)
	require.Equal(t, "base prompt", c.withSessionMode("base prompt"))
}

// A mode with no model role assigned still contributes its prompt -- it
// just runs on whatever model the session is already using.
func TestSessionModeModelIsAbsentWithoutAMatchingRole(t *testing.T) {
	c := newSessionModeCoordinator(t, "review")

	_, ok, err := c.sessionModeModel(t.Context())
	require.NoError(t, err)
	require.False(t, ok)
	require.Contains(t, c.withSessionMode("base prompt"), `<mode name="review">`)
}
