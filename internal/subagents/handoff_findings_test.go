package subagents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandoffFindingsCompatibility(t *testing.T) {
	t.Parallel()
	old := `{"task_id":"api","summary":"Inspected","changed_files":[],"checks":[],"risks":[],"dependencies":[],"decision":"passed"}`
	_, err := ParseHandoff(old, "api")
	require.NoError(t, err)
	var h Handoff
	require.NoError(t, json.Unmarshal([]byte(old), &h))
	h.Decision = "changes_required"
	h.Findings = []HandoffFinding{{Path: "api.go", StartLine: 2, EndLine: 1, Severity: 1, Issue: "Bug", Expected: "Valid result", Evidence: "Observed"}}
	data, err := json.Marshal(h)
	require.NoError(t, err)
	_, err = ParseHandoff(string(data), "api")
	require.Error(t, err)
	h.Findings[0].EndLine = 3
	data, err = json.Marshal(h)
	require.NoError(t, err)
	_, err = ParseHandoff(string(data), "api")
	require.NoError(t, err)
	h.Findings[0].Path = "../escape"
	data, err = json.Marshal(h)
	require.NoError(t, err)
	_, err = ParseHandoff(string(data), "api")
	require.Error(t, err)
}
