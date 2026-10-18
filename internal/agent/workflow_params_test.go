package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/stretchr/testify/require"
)

func TestNormalizeWorkflowParamsReadsFlattenedCalls(t *testing.T) {
	t.Parallel()

	p := normalizeWorkflowParams(WorkflowParams{Action: "contract_add"}, `{"action":"contract_add"}`)
	require.Equal(t, "work", p.Action)
	require.Equal(t, "contract_add", p.WorkAction)

	// status and budget are workflow actions of their own.
	p = normalizeWorkflowParams(WorkflowParams{Action: "status"}, `{"action":"status"}`)
	require.Equal(t, "status", p.Action)
	require.Empty(t, p.WorkAction)

	raw := `{"action":"plan","profile":"web","requirements":[{"id":"R1","text":"hero"}],"stages":[{"name":"build"}]}`
	p = normalizeWorkflowParams(WorkflowParams{Action: "plan"}, raw)
	require.NotNil(t, p.Plan)
	require.Len(t, p.Plan.Requirements, 1)

	raw = `{"action":"design","audience":"space fans","primary_flow":"explore"}`
	p = normalizeWorkflowParams(WorkflowParams{Action: "design"}, raw)
	require.NotNil(t, p.Design)
	require.Equal(t, "space fans", p.Design.Audience)

	p = normalizeWorkflowParams(WorkflowParams{Action: "plan"}, `{"action":"plan"}`)
	require.Nil(t, p.Plan)
}

func TestUserRequestTextIncludesPastedText(t *testing.T) {
	t.Parallel()
	call := SessionAgentCall{Prompt: "  ", Attachments: []message.Attachment{
		{FileName: "paste_1.txt", MimeType: "text/plain", Content: []byte("build a solar system")},
		{FileName: "notes.txt", FilePath: "notes.txt", MimeType: "text/plain", Content: []byte("a file")},
	}}
	require.Equal(t, "build a solar system", userRequestText(call))
}
