package tools

import (
	"encoding/json"
	"slices"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
)

// pipelineEffects retains dispatch evidence across all nested steps, including
// successful steps before a pause and uncertain steps before a failed return.
type pipelineEffects struct {
	sent     bool
	unknown  bool
	subjects []string
}

func (p *pipelineEffects) observe(call fantasy.ToolCall, response fantasy.ToolResponse, err error) {
	native, known := ResponseOutcome(response)
	mutates := CallMutates(call.Name, call.Input)
	if known {
		mutates = native.Mutation
	}
	if !mutates || known && native.Dispatch == "none" {
		return
	}
	p.sent = true
	p.unknown = p.unknown || err != nil || native.Dispatch == "unknown" || !known && (response.IsError || response.StopTurn)
	subjects := []string{"*"}
	if known && len(native.Subjects) > 0 {
		subjects = native.Subjects
	}
	for _, subject := range subjects {
		if subject == "" || subject == "desktop:" {
			subject = "*"
		}
		if !slices.Contains(p.subjects, subject) {
			p.subjects = append(p.subjects, subject)
		}
	}
}

func (p *pipelineEffects) response(response fantasy.ToolResponse) fantasy.ToolResponse {
	if native, ok := ResponseOutcome(response); ok && native.Source == "tool_pipeline:desktop_flow" {
		// Durable desktop flows also account for inherited pending intents,
		// which a tracker of only this invocation cannot reconstruct.
		return response
	}
	aggregate := ToolOutcome{Dispatch: "none", Source: "tool_pipeline", Kind: outcome.KindObservation, Claim: "pipeline"}
	if p.sent {
		aggregate.Dispatch, aggregate.Mutation = "sent", true
		aggregate.Kind, aggregate.Subjects = outcome.KindOutput, p.subjects
	}
	if p.unknown {
		aggregate.Dispatch = "unknown"
	}
	// The last child's check is not proof of the whole workflow. Preserve its
	// native result separately while leaving question/permission data intact.
	var metadata map[string]json.RawMessage
	if json.Unmarshal([]byte(response.Metadata), &metadata) == nil && metadata != nil {
		if child, ok := metadata["atlas_outcome"]; ok {
			if _, collision := metadata["pipeline_child_atlas_outcome"]; collision {
				metadata["pipeline_parent_metadata"] = json.RawMessage(strconvJSON(response.Metadata))
			}
			metadata["pipeline_child_atlas_outcome"] = child
			response = fantasy.WithResponseMetadata(response, metadata)
		}
	}
	return withToolOutcome(response, aggregate)
}
