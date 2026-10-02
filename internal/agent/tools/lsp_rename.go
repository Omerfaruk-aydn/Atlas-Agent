package tools

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filetracker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	lsputil "github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

type RenameParams struct {
	Preview bool   `json:"preview,omitempty" description:"Prepare a source-checked plan and exact diff without changing files"`
	Symbol  string `json:"symbol" description:"The symbol name to rename"`
	NewName string `json:"new_name" description:"The new name for the symbol"`
	Path    string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
}

const RenameToolName = "lsp_rename"

//go:embed lsp_rename.md
var renameDescription string

func NewRenameTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	options ...SemanticEditServices,
) fantasy.AgentTool {
	services := semanticServices("", permissions, files, filetracker, lspManager, options)
	return fantasy.NewAgentTool(
		RenameToolName,
		renameDescription,
		func(ctx context.Context, params RenameParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Symbol == "" {
				return fantasy.NewTextErrorResponse("symbol is required"), nil
			}
			if params.NewName == "" {
				return fantasy.NewTextErrorResponse("new_name is required"), nil
			}
			root, err := services.sourceRoot(ctx)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			workingDir := root
			if params.Path != "" {
				workingDir = resolvePath(root, params.Path)
			}
			resolved, err := resolveSymbol(ctx, lspManager, params.Symbol, workingDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Symbol '%s' not found", params.Symbol)), nil
			}
			_, sourceHash, err := lsputil.ReadEditSource(ctx, root, resolved.path)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			edit, err := resolved.client.Rename(ctx, resolved.path, resolved.line, resolved.char, params.NewName)
			if err != nil {
				slog.Error("Failed to rename symbol", "error", err, "symbol", params.Symbol)
				return fantasy.NewTextErrorResponse(fmt.Sprintf("rename failed: %s", err)), nil
			}
			if edit == nil {
				return fantasy.NewTextResponse(fmt.Sprintf("No rename edits generated for symbol '%s'", params.Symbol)), nil
			}

			plan, err := lsputil.PrepareWorkspaceEdit(ctx, root, *edit, resolved.client.GetOffsetEncoding())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := semanticSourceUnchanged(ctx, root, resolved.path, sourceHash); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return services.submit(ctx, plan, params.Preview, call)
		},
	)
}
