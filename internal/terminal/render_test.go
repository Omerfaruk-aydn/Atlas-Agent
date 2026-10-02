package terminal

import (
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/stretchr/testify/require"
)

func TestTerminalRenderingControlsRemainSeparateFromRawEvidence(t *testing.T) {
	t.Parallel()
	raw := []byte("old\rnew\x1b[2;1Hsecond\x1b[2K\r界\x1b]52;c;NEVER_EXECUTE\a\x1b[3;1Hsafe")
	screen, err := RenderTranscript(t.Context(), raw, execution.TerminalSize{Width: 20, Height: 5})
	require.NoError(t, err)
	require.Equal(t, "new", screen.Lines[0])
	require.Equal(t, "界", screen.Lines[1])
	require.Equal(t, "safe", screen.Lines[2])
	require.Empty(t, screen.Unsupported)
	require.NotContains(t, strings.Join(screen.Lines, "\n"), "NEVER_EXECUTE")
	require.Contains(t, string(raw), "NEVER_EXECUTE")
	screen, err = RenderTranscript(t.Context(), []byte("\x1b[?1049htext"), execution.TerminalSize{Width: 20, Height: 5})
	require.NoError(t, err)
	require.NotEmpty(t, screen.Unsupported)
}
