package tools

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/filetracker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
	lsputil "github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

type ReplaceSymbolParams struct {
	Preview     bool   `json:"preview,omitempty" description:"Prepare a source-checked plan and exact diff without changing files"`
	Symbol      string `json:"symbol" description:"The symbol name to target (e.g., function name, method name, type name)"`
	FilePath    string `json:"file_path" description:"The path to the file containing the symbol"`
	Replacement string `json:"replacement,omitempty" description:"The replacement text. Required for 'replace' action. For 'add_before'/'add_after', the text to insert. Ignored for 'delete'."`
	Action      string `json:"action,omitempty" description:"Operation to perform: 'replace' (default, replace entire symbol), 'add_before' (insert before symbol), 'add_after' (insert after symbol), 'delete' (remove symbol entirely)"`
}

const ReplaceSymbolToolName = "lsp_replace_symbol"

//go:embed lsp_replace_symbol.md
var replaceSymbolDescription string

// ReplaceSymbolResponseMetadata carries diff data for the renderer.
type ReplaceSymbolResponseMetadata struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
	Action     string `json:"action"`
}

// ReplaceSymbolPermissionsParams carries diff data for the permission dialog.
type ReplaceSymbolPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
}

func NewReplaceSymbolTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	options ...SemanticEditServices,
) fantasy.AgentTool {
	services := semanticServices("", permissions, files, filetracker, lspManager, options)
	return fantasy.NewAgentTool(
		ReplaceSymbolToolName,
		replaceSymbolDescription,
		func(ctx context.Context, params ReplaceSymbolParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Symbol == "" {
				return fantasy.NewTextErrorResponse("symbol is required"), nil
			}
			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path is required"), nil
			}

			action := params.Action
			if action == "" {
				action = "replace"
			}
			switch action {
			case "replace", "add_before", "add_after", "delete":
			default:
				return fantasy.NewTextErrorResponse(fmt.Sprintf("invalid action %q: must be replace, add_before, add_after, or delete", action)), nil
			}
			if (action == "replace" || action == "add_before" || action == "add_after") && params.Replacement == "" {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("replacement is required for action %q", action)), nil
			}

			root, err := services.sourceRoot(ctx)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			params.FilePath = resolvePath(root, params.FilePath)
			lspManager.Start(ctx, params.FilePath)

			client := findLSPClient(lspManager, params.FilePath)
			if client == nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("no LSP client handles file: %s", params.FilePath)), nil
			}

			_, sourceHash, err := lsputil.ReadEditSource(ctx, root, params.FilePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			symbols, err := client.DocumentSymbols(ctx, params.FilePath)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("failed to get document symbols: %s", err)), nil
			}

			target := findSymbolByName(symbols, params.Symbol)
			if target == nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("symbol '%s' not found in %s", params.Symbol, params.FilePath)), nil
			}

			rng := target.GetRange()

			text := params.Replacement
			switch action {
			case "add_before":
				rng.End = rng.Start
				text += "\n"
			case "add_after":
				rng.Start = rng.End
				text = "\n" + text
			case "delete":
				text = ""
			}
			edit := protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{protocol.URIFromPath(params.FilePath): {{Range: rng, NewText: text}}}}
			plan, err := lsputil.PrepareWorkspaceEdit(ctx, root, edit, client.GetOffsetEncoding())
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if err := semanticSourceUnchanged(ctx, root, params.FilePath, sourceHash); err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return services.submit(ctx, plan, params.Preview, call)
		},
	)
}

// findSymbolByName searches for a symbol by name in the document symbol tree.
func findSymbolByName(symbols []protocol.DocumentSymbolResult, name string) protocol.DocumentSymbolResult {
	for _, sym := range symbols {
		if sym.GetName() == name {
			return sym
		}
		if ds, ok := sym.(*protocol.DocumentSymbol); ok && len(ds.Children) > 0 {
			children := make([]protocol.DocumentSymbolResult, len(ds.Children))
			for i := range ds.Children {
				children[i] = &ds.Children[i]
			}
			if found := findSymbolByName(children, name); found != nil {
				return found
			}
		}
	}
	return nil
}
