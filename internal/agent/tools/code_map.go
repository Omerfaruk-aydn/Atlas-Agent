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

