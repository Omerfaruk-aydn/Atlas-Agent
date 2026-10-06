package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDesktopEvidenceNeverInfersClosureFromIncompleteData(t *testing.T) {
	t.Parallel()
	for _, content := range []string{`{}`, `{"result":null}`, `{"result":{}}`, `{"result":[{}]}`, "invalid"} {
		require.Nil(t, summarizeDesktopEvidence("windows", content, []string{"11"}))
	}
	windows := make([]desktopWindowInfo, 500)
	for i := range windows {
		windows[i].ID = "22"
	}
	data, err := json.Marshal(map[string]any{"result": windows})
	require.NoError(t, err)
	e := summarizeDesktopEvidence("windows", string(data), []string{"11"})
	require.NotNil(t, e)
	require.Empty(t, e.AbsentClosed, "A capped native enumeration is not evidence of closure")
	e = summarizeDesktopEvidence("windows", `{"result":[{"window_id":"11","foreground":true}]}`, []string{"11"})
	require.NotNil(t, e)
	require.Empty(t, e.AbsentClosed, "A successful close shortcut does not prove disappearance")
}
