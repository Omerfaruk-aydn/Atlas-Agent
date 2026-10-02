package tools

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filetracker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	lsputil "github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

const LspRenameFileToolName = "lsp_rename_file"

//go:embed lsp_rename_file.md
var lspRenameFileDescription string

type LspRenameFileParams struct {
	Preview bool   `json:"preview,omitempty" description:"Prepare a source-checked move/reference plan without changing files"`
	OldPath string `json:"old_path" description:"The file's current path."`
	NewPath string `json:"new_path" description:"Where it should end up. Must not already exist."`
}

func NewLspRenameFileTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
	options ...SemanticEditServices,
) fantasy.AgentTool {
	services := semanticServices(workingDir, permissions, files, filetracker, lspManager, options)
	return fantasy.NewAgentTool(
		LspRenameFileToolName,
		lspRenameFileDescription,
		func(ctx context.Context, params LspRenameFileParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.OldPath == "" {
				return fantasy.NewTextErrorResponse("old_path is required"), nil
			}
			if params.NewPath == "" {
				return fantasy.NewTextErrorResponse("new_path is required"), nil
			}

			root, err := services.sourceRoot(ctx)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			oldPath := resolvePath(root, params.OldPath)
			newPath := resolvePath(root, params.NewPath)

			if _, err := os.Stat(oldPath); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s does not exist", relOrAbs(oldPath, workingDir))), nil
			}
			if _, err := os.Stat(newPath); err == nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("%s already exists", relOrAbs(newPath, workingDir))), nil
			}

			lspManager.Start(ctx, filepath.Dir(oldPath))
			client := findLSPClient(lspManager, oldPath)
			if client == nil {
				return fantasy.NewTextErrorResponse(
					"no LSP client handles this file, so there is nothing this tool can check for cascading edits -- use the write or bash tools for a plain move",
				), nil
			}

			_, sourceHash, err := lsputil.ReadEditSource(ctx, root, oldPath)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			edit, err := client.WillRenameFiles(ctx, oldPath, newPath)
			if err != nil && !isMethodNotFoundError(err) {
				slog.Error("willRenameFiles request failed", "error", err, "old_path", oldPath, "new_path", newPath)
				return fantasy.NewTextErrorResponse(fmt.Sprintf("willRenameFiles failed: %s", err)), nil
			}

			if edit == nil {
				edit = &protocol.WorkspaceEdit{}
			}
			// Convert map edits into ordered document changes before appending
			// the actual move, so references and both move targets share one
			// preflight and journal rather than separate filesystem mutations.
			uris := make([]protocol.DocumentURI, 0, len(edit.Changes))
			for uri := range edit.Changes {
				uris = append(uris, uri)
			}
			slices.Sort(uris)
			for _, uri := range uris {
				text := &protocol.TextDocumentEdit{TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri}}}
				for _, change := range edit.Changes[uri] {
					text.Edits = append(text.Edits, protocol.Or_TextDocumentEdit_edits_Elem{Value: change})
				}
				edit.DocumentChanges = append(edit.DocumentChanges, protocol.DocumentChange{TextDocumentEdit: text})
			}
			edit.Changes = nil
			movePresent := false
			for _, change := range edit.DocumentChanges {
				if change.RenameFile != nil && change.RenameFile.OldURI == protocol.URIFromPath(oldPath) && change.RenameFile.NewURI == protocol.URIFromPath(newPath) {
					movePresent = true
				}
			}
			if !movePresent {
				edit.DocumentChanges = append(edit.DocumentChanges, protocol.DocumentChange{RenameFile: &protocol.RenameFile{Kind: "rename", OldURI: protocol.URIFromPath(oldPath), NewURI: protocol.URIFromPath(newPath)}})
			}
			plan, err := lsputil.PrepareWorkspaceEdit(ctx, root, *edit, client.GetOffsetEncoding())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := semanticSourceUnchanged(ctx, root, oldPath, sourceHash); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return services.submit(ctx, plan, params.Preview, call)
		},
	)
}

func resolvePath(workingDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workingDir, path)
}

func isMethodNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "method not found")
}

func workspaceEditEmpty(edit *protocol.WorkspaceEdit) bool {
	return len(edit.Changes) == 0 && len(edit.DocumentChanges) == 0
}
