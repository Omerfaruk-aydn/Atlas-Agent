package tools

import (
	"context"
	_ "embed"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codemap"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// CodeMapToolName is the name of the code map tool.
const CodeMapToolName = "code_map"

//go:embed code_map.md
var codeMapDescription string

// CodeMapParams are the code map tool's parameters.
type CodeMapParams struct {
	Action string `json:"action" description:"overview: the project's languages, subsystems, entry points and most depended-on files with their main definitions. find: definitions and files whose name matches query. explain: one definition or file and how it connects (who uses it, what it uses, imports, files changed together). callers: what changing query would affect, level by level. path: how query connects to to." enum:"overview,find,explain,callers,path"`
	Query  string `json:"query,omitempty" description:"A definition name (solveKepler), Container.Name (Orbit.step), path:Name (src/app.ts:main) or a file path. Required except for overview."`
	To     string `json:"to,omitempty" description:"For path: the definition or file to reach."`
	Depth  int    `json:"depth,omitempty" description:"For callers: how many levels of users to follow, 1-5, default 2."`
	Budget int    `json:"budget,omitempty" description:"For overview: about how many tokens to spend, default 2000, at most 8000."`
}

