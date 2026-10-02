package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	powernap "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	lsputil "github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

type countedEditPermission struct {
	*mockBashPermissionService
	paths     []string
	deny      int
	onRequest func(permission.CreatePermissionRequest)
}

func (p *countedEditPermission) Request(_ context.Context, request permission.CreatePermissionRequest) (bool, error) {
	p.paths = append(p.paths, request.Path)
	if p.onRequest != nil {
		p.onRequest(request)
	}
	return len(p.paths) != p.deny, nil
}

func TestEditPlanPermissionReviewRejectsSourceChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file.go")
	require.NoError(t, os.WriteFile(file, []byte("old"), 0o600))
	plan, err := lsputil.PrepareWorkspaceEdit(t.Context(), root, protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{protocol.URIFromPath(file): {{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}}}, powernap.UTF16)
	require.NoError(t, err)
	permissions := &countedEditPermission{onRequest: func(request permission.CreatePermissionRequest) {
		params, ok := request.Params.(SemanticEditPermissionsParams)
		require.True(t, ok)
		require.Equal(t, "old", params.OldContent)
		require.Equal(t, "new", params.NewContent)
		require.NoError(t, os.WriteFile(file, []byte("user edit"), 0o600))
	}}
	services := SemanticEditServices{Root: root, Store: engineering.NewStore(t.TempDir()), Permissions: permissions}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	response, err := services.submit(ctx, plan, false, fantasy.ToolCall{ID: "apply"})
	require.NoError(t, err)
	require.True(t, response.IsError)
	require.Contains(t, response.Content, "during permission review")
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "user edit", string(data))
}

func TestEditPlanRecoveryReconcilesAdmissionWithoutJournal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file.go")
	require.NoError(t, os.WriteFile(file, []byte("old"), 0o600))
	plan, err := lsputil.PrepareWorkspaceEdit(t.Context(), root, protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{protocol.URIFromPath(file): {{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}}}, powernap.UTF16)
	require.NoError(t, err)
	services := SemanticEditServices{Root: root, Store: engineering.NewStore(t.TempDir()), Permissions: &countedEditPermission{}}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	response, err := services.submit(ctx, plan, true, fantasy.ToolCall{ID: "preview"})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	meta, record, _, err := services.read(ctx, plan.ID)
	require.NoError(t, err)
	_, err = services.mark(ctx, meta, record, "applying")
	require.NoError(t, err)
	require.Error(t, services.Store.SemanticEditsReady(ctx, "session"))
	response, err = NewLSPEditPlanTool(services).Run(ctx, fantasy.ToolCall{ID: "recover", Input: `{"action":"recover","plan_id":"` + plan.ID + `"}`})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Contains(t, response.Content, "rolled_back")
	require.NoError(t, services.Store.SemanticEditsReady(ctx, "session"))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "old", string(data))
}

func TestEditPlanPermissionsAndOwnership(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first, second := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{}}
	for _, file := range []string{first, second} {
		require.NoError(t, os.WriteFile(file, []byte("old"), 0o600))
		edit.Changes[protocol.URIFromPath(file)] = []protocol.TextEdit{{Range: protocol.Range{End: protocol.Position{Character: 3}}, NewText: "new"}}
	}
	plan, err := lsputil.PrepareWorkspaceEdit(t.Context(), root, edit, powernap.UTF16)
	require.NoError(t, err)
	perms := &countedEditPermission{deny: 2}
	services := SemanticEditServices{Root: root, Store: engineering.NewStore(t.TempDir()), Permissions: perms}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "session")
	resp, err := services.submit(ctx, plan, false, fantasy.ToolCall{ID: "deny", Name: "lsp_rename"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Len(t, perms.paths, 2)
	for _, file := range []string{first, second} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, "old", string(data))
	}
	perms.paths, perms.deny = nil, 0
	owned := engineering.WithScope(ctx, "session", "owner")
	owned = engineering.WithOwnership(owned, root, []string{"a.go"})
	resp, err = services.submit(owned, plan, false, fantasy.ToolCall{ID: "outside-owner", Name: "lsp_rename"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Empty(t, perms.paths, "every target must pass ownership before any permission request")
	resp, err = services.submit(execution.WithReadOnly(ctx), plan, false, fantasy.ToolCall{ID: "readonly", Name: "lsp_rename"})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Empty(t, perms.paths)
	resp, err = services.submit(ctx, plan, true, fantasy.ToolCall{ID: "preview", Name: "lsp_rename"})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	var preview struct {
		PlanID string                  `json:"plan_id"`
		Diff   engineering.ArtifactRef `json:"diff_ref"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Content), &preview))
	require.Equal(t, plan.ID, preview.PlanID)
	_, err = services.Store.ReadArtifact(ctx, preview.Diff)
	require.NoError(t, err)
	tool := NewLSPEditPlanTool(services)
	input, err := json.Marshal(EditPlanParams{Action: "apply", PlanID: plan.ID})
	require.NoError(t, err)
	resp, err = tool.Run(ctx, fantasy.ToolCall{ID: "apply", Name: "lsp_edit_plan", Input: string(input)})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	for _, file := range []string{first, second} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, "new", string(data))
	}
	require.NoError(t, services.Store.SemanticEditsReady(ctx, "session"))
}
