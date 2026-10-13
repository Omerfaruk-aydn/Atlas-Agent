package tools

import (
	"context"
	"encoding/json"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/stretchr/testify/require"
)

func TestCredentialImageProofCannotAuthorizeOtherBytesOrTools(t *testing.T) {
	response := withCredentialImageProof(fantasy.NewImageResponse([]byte("synthetic-png"), "image/png"), 2)
	require.True(t, protectedCredentialMedia(BrowserToolName, response))
	require.True(t, protectedCredentialMedia("tool_pipeline", response))
	require.False(t, protectedCredentialMedia("mcp_untrusted", response))
	response.Data = []byte("different-image")
	require.False(t, protectedCredentialMedia(BrowserToolName, response))
}

func TestPipelinePreservesOnlyTheSelectedProtectedImage(t *testing.T) {
	for _, protected := range []bool{true, false} {
		tool := NewToolPipeline(func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
			response := fantasy.NewImageResponse([]byte("synthetic-selected-image"), "image/png")
			if protected {
				response = withCredentialImageProof(response, 3)
			}
			return response, nil
		})
		input, err := json.Marshal(PipelineParams{Steps: []PipelineStep{{ID: "screen", Tool: BrowserToolName, Arguments: map[string]any{"action": "screenshot", "annotate": false}}}})
		require.NoError(t, err)
		response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(input)})
		require.NoError(t, err)
		require.Equal(t, protected, protectedCredentialMedia("tool_pipeline", response))
		if protected {
			var meta struct {
				Proof credentialImageProof `json:"credential_image_redaction"`
			}
			require.NoError(t, json.Unmarshal([]byte(response.Metadata), &meta))
			require.Equal(t, 3, meta.Proof.Masks)
		}
	}
}

func TestPipelineRejectsChangedProtectedPixels(t *testing.T) {
	data := []byte("protected-selected-image")
	tool := NewToolPipeline(func(_ context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if call.Name == BrowserToolName {
			return withCredentialImageProof(fantasy.NewImageResponse(data, "image/png"), 1), nil
		}
		data[0] = 'X'
		return fantasy.NewTextResponse("Synthetic following step"), nil
	})
	input, err := json.Marshal(PipelineParams{Steps: []PipelineStep{
		{ID: "screen", Tool: BrowserToolName, Arguments: map[string]any{"action": "screenshot"}},
		{ID: "following", Tool: "view", Arguments: map[string]any{"file_path": "fixture.txt"}},
	}})
	require.NoError(t, err)
	response, err := tool.Run(t.Context(), fantasy.ToolCall{Input: string(input)})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Empty(t, response.Data)
	require.Contains(t, response.Content, "image_changed")
}

func TestCredentialFilteredScreenshotWorksAfterSignIn(t *testing.T) {
	l := newLiveTool(t)
	t.Cleanup(func() { vault.Ephemeral.PurgeSession(l.id) })
	require.NoError(t, vault.Register(l.id, "Synthetic-Image-Privacy-88!"))
	l.ok(`{"action":"navigate","url":"` + l.base + `/design.html"}`)
	l.tool = WithSecretFilter(l.tool)
	response := l.ok(`{"action":"screenshot","annotate":false}`)
	require.Equal(t, "image", response.Type)
	require.NotEmpty(t, response.Data)
	require.True(t, protectedCredentialMedia(BrowserToolName, response))
	// Non-browser media without a native redaction proof stays blocked.
	fixture := fantasy.NewAgentTool("unprotected_fixture", "Fixture.", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewImageResponse([]byte("unprotected-png"), "image/png"), nil
	})
	response, err := WithSecretFilter(fixture).Run(l.ctx, fantasy.ToolCall{Input: "{}"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Empty(t, response.Data)
}
