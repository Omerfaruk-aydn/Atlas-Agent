package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed tool_pipeline.md
var pipelineDescription string

type PipelineStep struct {
	ID        string         `json:"id"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Items     []string       `json:"items,omitempty"`
	IfSuccess string         `json:"if_success,omitempty"`
}
type (
	PipelineParams struct {
		Steps  []PipelineStep `json:"steps"`
		Return []string       `json:"return,omitempty"`
	}
	PipelineResult struct {
		ID        string `json:"id"`
		Iteration int    `json:"iteration"`
		IsError   bool   `json:"is_error"`
		Content   string `json:"content"`
		Metadata  string `json:"metadata,omitempty"`
	}
)

func NewToolPipeline(invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) fantasy.AgentTool {
	return fantasy.NewAgentTool("tool_pipeline", pipelineDescription, func(ctx context.Context, p PipelineParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if len(p.Steps) < 1 || len(p.Steps) > 32 {
			return fantasy.NewTextErrorResponse("pipeline requires 1-32 steps"), nil
		}
		seen := map[string]bool{}
		count := 0
		for _, s := range p.Steps {
			if s.ID == "" || seen[s.ID] || s.Tool == "" || s.Tool == "tool_pipeline" || s.Tool == "agent_jobs" || s.Tool == "task_board" || s.Tool == "goal" {
				return fantasy.NewTextErrorResponse("invalid step identity or recursive/control tool"), nil
			}
			if s.IfSuccess != "" && !seen[s.IfSuccess] {
				return fantasy.NewTextErrorResponse("if_success must reference an earlier step"), nil
			}
			seen[s.ID] = true
			count += max(1, len(s.Items))
		}
		if count > 64 {
			return fantasy.NewTextErrorResponse("pipeline is limited to 64 invocations"), nil
		}
		for _, id := range p.Return {
			if !seen[id] {
				return fantasy.NewTextErrorResponse("unknown return step"), nil
			}
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		results := []PipelineResult{}
		success := map[string]bool{}
		for _, s := range p.Steps {
			if s.IfSuccess != "" && !success[s.IfSuccess] {
				continue
			}
			items := s.Items
			if len(items) == 0 {
				items = []string{""}
			}
			success[s.ID] = true
			for i, item := range items {
				if err := ctx.Err(); err != nil {
					return fantasy.ToolResponse{}, err
				}
				encoded, err := json.Marshal(substitutePipeline(s.Arguments, item))
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if len(encoded) > 64*1024 {
					return fantasy.NewTextErrorResponse("pipeline arguments exceed limit"), nil
				}
				response, err := invoke(ctx, fantasy.ToolCall{ID: fmt.Sprintf("%s/%s/%d", call.ID, s.ID, i), Name: s.Tool, Input: string(encoded)})
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				content := response.Content
				if len(content) > 8000 {
					content = content[:8000] + "\n[truncated]"
				}
				metadata := response.Metadata
				if len(metadata) > 4000 {
					metadata = "[metadata omitted: exceeds 4 KiB]"
				}
				results = append(results, PipelineResult{ID: s.ID, Iteration: i, IsError: response.IsError, Content: content, Metadata: metadata})
				if response.IsError {
					success[s.ID] = false
				}
				if response.StopTurn {
					return response, nil
				}
				if response.IsError {
					data, _ := json.Marshal(results)
					return fantasy.NewTextErrorResponse(string(data)), nil
				}
			}
		}
		returned := results
		if len(p.Return) > 0 {
			returned = nil
			for _, r := range results {
				for _, id := range p.Return {
					if id == r.ID {
						returned = append(returned, r)
						break
					}
				}
			}
		}
		data, err := json.Marshal(returned)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if len(data) > 64*1024 {
			return fantasy.NewTextErrorResponse("selected pipeline output exceeds 64 KiB; select fewer return steps"), nil
		}
		return fantasy.NewTextResponse(string(data)), nil
	})
}

func substitutePipeline(v any, item string) any {
	switch value := v.(type) {
	case string:
		if value == "$item" {
			return item
		}
		return value
	case map[string]any:
		out := map[string]any{}
		for k, v := range value {
			out[k] = substitutePipeline(v, item)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = substitutePipeline(v, item)
		}
		return out
	default:
		return v
	}
}
