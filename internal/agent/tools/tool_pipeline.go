package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

//go:embed tool_pipeline.md
var pipelineDescription string

type PipelineStep struct {
	RequirePassed   bool           `json:"require_passed,omitempty" description:"Require a computer assert response with passed:true before continuing."`
	ObservationFrom string         `json:"observation_from,omitempty" description:"Earlier computer observe step supplying snapshot_id for this computer call."`
	ID              string         `json:"id"`
	Tool            string         `json:"tool"`
	Arguments       map[string]any `json:"arguments"`
	Items           []string       `json:"items,omitempty"`
	IfSuccess       string         `json:"if_success,omitempty"`
}
type (
	PipelineParams struct {
		Desktop *DesktopWorkflowParams `json:"desktop,omitempty" description:"Use a bounded desktop recipe instead of steps. Preferred for prepare, input plus observation, or verified field submission."`
		Steps   []PipelineStep         `json:"steps,omitempty"`
		Return  []string               `json:"return,omitempty"`
	}
	PipelineResult struct {
		ID            string `json:"id"`
		Iteration     int    `json:"iteration"`
		IsError       bool   `json:"is_error"`
		Content       string `json:"content"`
		Metadata      string `json:"metadata,omitempty"`
		MediaType     string `json:"media_type,omitempty"`
		ImageAttached bool   `json:"image_attached,omitempty"`
		ImageOmitted  bool   `json:"image_omitted,omitempty"`
	}
)

func NewToolPipeline(invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) fantasy.AgentTool {
	return fantasy.NewAgentTool("tool_pipeline", pipelineDescription, func(ctx context.Context, p PipelineParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		if p.Desktop != nil {
			if len(p.Steps) > 0 || len(p.Return) > 0 {
				return fantasy.NewTextErrorResponse("desktop recipes cannot be combined with steps or return"), nil
			}
			return runDesktopWorkflow(ctx, *p.Desktop, call, invoke)
		}
		if len(p.Steps) < 1 || len(p.Steps) > 32 {
			return fantasy.NewTextErrorResponse("pipeline requires 1-32 steps"), nil
		}
		seen := map[string]bool{}
		observations := map[string]bool{}
		count := 0
		for _, s := range p.Steps {
			if s.RequirePassed && (s.Tool != ComputerToolName || s.Arguments["action"] != "assert") {
				return fantasy.NewTextErrorResponse("require_passed requires a computer assert step"), nil
			}
			if s.ObservationFrom != "" && (s.Tool != ComputerToolName || !observations[s.ObservationFrom] || len(s.Items) > 0) {
				return fantasy.NewTextErrorResponse("observation_from requires an earlier single computer observe step"), nil
			}
			if s.ID == "" || seen[s.ID] || s.Tool == "" || s.Tool == "tool_pipeline" || s.Tool == "agent_jobs" || s.Tool == "task_board" || s.Tool == "goal" {
				return fantasy.NewTextErrorResponse("invalid step identity or recursive/control tool"), nil
			}
			if s.IfSuccess != "" && !seen[s.IfSuccess] {
				return fantasy.NewTextErrorResponse("if_success must reference an earlier step"), nil
			}
			seen[s.ID] = true
			observations[s.ID] = s.Tool == ComputerToolName && s.Arguments["action"] == "observe" && len(s.Items) == 0
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
		var imageData []byte
		var imageMediaType string
		imageIndex := -1
		success := map[string]bool{}
		snapshots := map[string]string{}
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
				arguments := substitutePipeline(s.Arguments, item)
				if s.ObservationFrom != "" {
					id := snapshots[s.ObservationFrom]
					if id == "" {
						return fantasy.NewTextErrorResponse("observation_from: missing snapshot_id; observe again"), nil
					}
					args, ok := arguments.(map[string]any)
					if !ok {
						return fantasy.NewTextErrorResponse("observation_from requires computer arguments"), nil
					}
					args["snapshot_id"] = id
				}
				encoded, err := json.Marshal(arguments)
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
				if !response.IsError && observations[s.ID] {
					var snapshot struct {
						ID string `json:"snapshot_id"`
					}
					if json.Unmarshal([]byte(response.Content), &snapshot) == nil {
						snapshots[s.ID] = snapshot.ID
					}
				}
				if s.RequirePassed && !response.IsError {
					var assertion struct {
						Passed bool `json:"passed"`
					}
					if json.Unmarshal([]byte(response.Content), &assertion) != nil || !assertion.Passed {
						response = fantasy.NewTextErrorResponse("gate_failed: " + s.ID + "; expected passed:true; subsequent steps were not executed")
					}
				}
				content := response.Content
				if len(content) > 8000 && !observations[s.ID] {
					content = content[:8000] + "\n[truncated]"
				}
				metadata := response.Metadata
				if len(metadata) > 4000 {
					metadata = "[metadata omitted: exceeds 4 KiB]"
				}
				results = append(results, PipelineResult{ID: s.ID, Iteration: i, IsError: response.IsError, Content: content, Metadata: metadata})
				if response.Type == "image" && len(response.Data) > 0 {
					index := len(results) - 1
					results[index].MediaType = response.MediaType
					results[index].ImageOmitted = true
					if !response.IsError && (len(p.Return) == 0 || slices.Contains(p.Return, s.ID)) {
						if len(response.Data) > 8*1024*1024 {
							return fantasy.NewTextErrorResponse("pipeline image exceeds 8 MiB; request a smaller crop and observe current state before retrying"), nil
						}
						// Keep one selected image without retaining intermediate frames.
						imageData, imageMediaType, imageIndex = response.Data, response.MediaType, index
					}
				}
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
		if imageIndex >= 0 {
			results[imageIndex].ImageAttached = true
			results[imageIndex].ImageOmitted = false
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
		if imageIndex >= 0 {
			response := fantasy.NewImageResponse(imageData, imageMediaType)
			response.Content = string(data)
			return response, nil
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
