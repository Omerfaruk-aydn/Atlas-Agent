package runoutput

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamWriterEmitsOneJSONObjectPerLine(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	writer := NewStreamWriter(&output)
	require.NoError(t, writer.Event("message_delta", map[string]any{"message_id": "m1", "delta": "line one\nline two"}))
	require.NoError(t, writer.Event("result", map[string]any{"status": "success"}))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		require.NotEmpty(t, event["type"])
	}
}

func TestExitErrorPreservesCause(t *testing.T) {
	t.Parallel()
	err := &ExitError{Code: ExitBlocked, Err: io.ErrClosedPipe}
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.Equal(t, ExitBlocked, err.Code)
}
