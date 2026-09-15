package permission

// ToolCategory classifies a tool by risk, driving ModeAutoAcceptEdits and
// ModePlan's decisions in Request(). This intentionally lives in the
// permission package (not agent/tools) because agent/tools already imports
// permission — the reverse import would cycle. Tool name strings are
// duplicated from their ToolName constants in internal/agent/tools/*.go by
// necessity; keep this table in sync if a tool is renamed.
type ToolCategory string

const (
	// CategoryReadOnly tools never mutate anything and are always safe to
	// run in ModePlan (glob/grep never even call Request; the rest are
	// listed for documentation completeness and to mirror config.go's
	// resolveReadOnlyTools).
	CategoryReadOnly ToolCategory = "read_only"
	// CategoryEdit tools touch the filesystem and are auto-approved under
	// ModeAutoAcceptEdits. view/ls only ever reach Request() for a path
	// outside the working directory, which is exactly the case where
	// "auto-accept edits" should also cover them.
	CategoryEdit ToolCategory = "edit"
	// CategoryExecute tools run arbitrary code/commands.
	CategoryExecute ToolCategory = "execute"
	// CategoryNetwork tools reach outside the local machine (HTTP fetch,
	// downloads, MCP tool calls) — grouped with Execute's "always prompt
	// outside Manual/Bypass" treatment but kept distinct for clarity.
	CategoryNetwork ToolCategory = "network"
)

// toolCategories is keyed by tool name (see internal/agent/tools/*.go's
// *ToolName constants). MCP tool names are per-server/dynamic and
// deliberately absent — CategoryForTool's default (CategoryExecute) means
// an MCP call is never silently auto-approved.
var toolCategories = map[string]ToolCategory{
	"glob":               CategoryReadOnly,
	"grep":               CategoryReadOnly,
	"lsp_call_hierarchy": CategoryReadOnly,
	"lsp_definition":     CategoryReadOnly,
	"lsp_symbols":        CategoryReadOnly,
	"lsp_references":     CategoryReadOnly,
	"lsp_diagnostics":    CategoryReadOnly,
	"sourcegraph":        CategoryReadOnly,
	"Atlas-Agent_info":   CategoryReadOnly,
	"Atlas-Agent_logs":   CategoryReadOnly,
	"todos":              CategoryReadOnly,
	"question":           CategoryReadOnly,
	"job_output":         CategoryReadOnly,
	"view":               CategoryEdit,
	"ls":                 CategoryEdit,
	"edit":               CategoryEdit,
	"write":              CategoryEdit,
	"multiedit":          CategoryEdit,
	"bash":               CategoryExecute,
	"job_kill":           CategoryExecute,
	"lsp_rename":         CategoryExecute,
	"lsp_replace_symbol": CategoryExecute,
	"lsp_restart":        CategoryExecute,
	"computer":           CategoryExecute,
	"fetch":              CategoryNetwork,
	"web_fetch":          CategoryNetwork,
	"web_search":         CategoryNetwork,
	"agentic_fetch":      CategoryNetwork,
	"download":           CategoryNetwork,
	"read_mcp_resource":  CategoryNetwork,
	"list_mcp_resources": CategoryNetwork,
}

// CategoryForTool returns toolName's risk category. Unknown names
// (including every dynamic MCP tool name) default to CategoryExecute, the
// most conservative bucket — never silently auto-approved outside
// ModeBypass.
func CategoryForTool(toolName string) ToolCategory {
	if c, ok := toolCategories[toolName]; ok {
		return c
	}
	return CategoryExecute
}
