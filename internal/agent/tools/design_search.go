package tools

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
)

const DesignSearchToolName = "design_search"

const maxDesignSearchResponseBytes = 32 * 1024

type designSearchOutput struct {
	Results   map[string][]skills.DesignMatch `json:"results"`
	Truncated bool                            `json:"truncated"`
	Omitted   int                             `json:"omitted_results"`
	MaxBytes  int                             `json:"max_bytes"`
}

//go:embed design_search.md
var designSearchDescription string

type DesignSearchParams struct {
	Query        string `json:"query" description:"English design keywords; limited Turkish aliases supported."`
	Domain       string `json:"domain" description:"style, color, typography, chart, ux, landing, product, icons, reasoning, react-performance, web-interface, or stack."`
	Stack        string `json:"stack,omitempty" description:"Required only for domain stack; use a documented stack name."`
	Limit        int    `json:"limit,omitempty" description:"Results per domain, 1 to 20; defaults to 5."`
	DesignSystem bool   `json:"design_system,omitempty" description:"Return candidates from product, style, color, landing, typography, reasoning and ux domains as a design brief starting point."`
}

// NewDesignSearchTool creates the offline, read-only design reference tool.
func NewDesignSearchTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(DesignSearchToolName, designSearchDescription,
		func(ctx context.Context, params DesignSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			domains := []string{params.Domain}
			if params.DesignSystem {
				domains = []string{"product", "style", "color", "landing", "typography", "reasoning", "ux"}
			}
			result := map[string][]skills.DesignMatch{}
			for _, domain := range domains {
				if err := ctx.Err(); err != nil {
					return fantasy.ToolResponse{}, err
				}
				stack := params.Stack
				if params.DesignSystem {
					stack = ""
				}
				matches, err := skills.SearchDesign(params.Query, domain, stack, params.Limit)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				result[domain] = matches
			}
			data, err := boundedDesignOutput(result, domains)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return fantasy.NewTextResponse(string(data)), nil
		})
}

func boundedDesignOutput(result map[string][]skills.DesignMatch, domains []string) ([]byte, error) {
	output := designSearchOutput{Results: result, MaxBytes: maxDesignSearchResponseBytes}
	for round := 0; ; round++ {
		data, err := json.Marshal(output)
		if err != nil {
			return nil, err
		}
		if len(data) <= maxDesignSearchResponseBytes {
			return data, nil
		}
		// Remove lower-ranked rows across domains in a stable round-robin order.
		for step := range len(domains) {
			domain := domains[(round+step)%len(domains)]
			rows := result[domain]
			if len(rows) > 0 {
				result[domain] = rows[:len(rows)-1]
				output.Truncated = true
				output.Omitted++
				break
			}
		}
	}
}
