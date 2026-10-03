package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

//go:embed code_query.md
var codeQueryDescription string

type CodeQueryParams struct {
	Action   string `json:"action" description:"symbols, definition, references, incoming or outgoing."`
	FilePath string `json:"file_path" description:"Literal workspace-relative source file."`
	Symbol   string `json:"symbol,omitempty" description:"Symbol name for references and call hierarchy."`
}

func NewCodeQueryTool(root string, invoke ToolInvoker) fantasy.AgentTool {
	return fantasy.NewAgentTool("code_query", codeQueryDescription, func(ctx context.Context, p CodeQueryParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if _, err := engineering.ReadProjectEvidence(ctx, root, p.FilePath); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		path := filepath.Join(root, p.FilePath)
		var name string
		var input any
		switch p.Action {
		case "symbols":
			name, input = SymbolsToolName, SymbolsParams{FilePath: path}
		case "references":
			name, input = ReferencesToolName, ReferencesParams{Symbol: p.Symbol, Path: path}
		case "incoming", "outgoing":
			name, input = CallHierarchyToolName, CallHierarchyParams{Symbol: p.Symbol, Path: path, Direction: p.Action}
		case "definition":
			name, input = DefinitionToolName, DefinitionParams{Symbol: p.Symbol, Path: path}
		default:
			return fantasy.NewTextErrorResponse("use symbols, definition, references, incoming or outgoing"), nil
		}
		if p.Action != "symbols" && strings.TrimSpace(p.Symbol) == "" {
			return fantasy.NewTextErrorResponse("symbol is required"), nil
		}
		data, err := json.Marshal(input)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if invoke == nil {
			return fantasy.NewTextErrorResponse("guarded LSP dispatch unavailable"), nil
		}
		return invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s-code-query", call.ID), Name: name, Input: string(data)})
	})
}
