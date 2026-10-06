package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/stretchr/testify/require"
)

func computerBatchTestParams(groups, inputs int) ComputerParams {
	p := ComputerParams{Action: "batch", Batch: &ComputerBatchParams{}}
	for range groups {
		group := ComputerBatchGroup{Checkpoint: computer.AutomationRequest{WindowID: "11", ElementID: "result", Condition: "text", Expected: "done"}}
		for range inputs {
			group.Inputs = append(group.Inputs, ComputerBatchInput{Action: "key", Key: "enter", Automation: computer.AutomationRequest{WindowID: "11"}})
		}
		p.Batch.Groups = append(p.Batch.Groups, group)
	}
	return p
}

func TestComputerBatchExecutesLongGroupsWithReadbacksWithoutImages(t *testing.T) {
	for _, size := range [][2]int{{1, 16}, {24, 1}, {6, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			inputs, assertions, enumerations := 0, 0, 0
			r, err := runComputerBatch(t.Context(), computerBatchTestParams(size[0], size[1]), fantasy.ToolCall{ID: "batch"}, func(_ context.Context, c fantasy.ToolCall) (fantasy.ToolResponse, error) {
				var p ComputerParams
				require.NoError(t, json.Unmarshal([]byte(c.Input), &p))
				require.Nil(t, p.Batch, "Children must be atomic calls")
				switch p.Action {
				case "key":
					inputs++
					return fantasy.NewTextResponse(`{}`), nil
				case "assert":
					assertions++
					return fantasy.NewTextResponse(`{"window_id":"11","passed":true,"actual":"done"}`), nil
				case "windows":
					enumerations++
					return fantasy.NewTextResponse(`{"result":[{"window_id":"11","foreground":true}]}`), nil
				default:
					t.Fatalf("Unnecessary batch action: %s", p.Action)
					return fantasy.ToolResponse{}, nil
				}
			})
			require.NoError(t, err)
			require.False(t, r.IsError, r.Content)
			require.Equal(t, size[0]*size[1], inputs)
			require.Equal(t, size[0], assertions)
			require.Equal(t, 1, enumerations)
			require.Equal(t, "text", r.Type)
		})
	}
}

type batchPermissionService struct {
	mockPermissionService
	actions []string
	deny    string
}

func (p *batchPermissionService) Request(_ context.Context, r permission.CreatePermissionRequest) (bool, error) {
	p.actions = append(p.actions, r.Action)
	return r.Action != p.deny, nil
}
