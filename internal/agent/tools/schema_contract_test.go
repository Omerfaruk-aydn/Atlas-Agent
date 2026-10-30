package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/stretchr/testify/require"
)

// withoutEnums drops enum constraints, so a zero value (an empty string for an
// enum field) does not stand in the way of checking structure and types.
func withoutEnums(node any) {
	switch v := node.(type) {
	case map[string]any:
		delete(v, "enum")
		for _, child := range v {
			withoutEnums(child)
		}
	case []any:
		for _, child := range v {
			withoutEnums(child)
		}
	}
}

// Every value a model can legitimately send is a value the Go type itself
// marshals, so the contract built from the type must accept it. This runs the
// generator and the validator against the real parameter types of
// representative tools, including ones with raw JSON, interface and custom
// decoding fields.
func TestToolContractAcceptsWhatTheGoTypeMarshals(t *testing.T) {
	t.Parallel()

	types := map[string]reflect.Type{
		"edit":        reflect.TypeFor[EditParams](),
		"write":       reflect.TypeFor[WriteParams](),
		"multiedit":   reflect.TypeFor[MultiEditParams](),
		"bash":        reflect.TypeFor[BashParams](),
		"view":        reflect.TypeFor[ViewParams](),
		"grep":        reflect.TypeFor[GrepParams](),
		"test_run":    reflect.TypeFor[TestRunParams](),
		"lint_run":    reflect.TypeFor[LintRunParams](),
		"verify":      reflect.TypeFor[VerifyParams](),
		"browser":     reflect.TypeFor[BrowserParams](),
		"computer":    reflect.TypeFor[ComputerParams](),
		"cli_bridge":  reflect.TypeFor[CLIBridgeParams](),
		"worktree":    reflect.TypeFor[WorktreeParams](),
		"tool_search": reflect.TypeFor[ToolSearchParams](),
		"todos":       reflect.TypeFor[TodosParams](),
		"fetch":       reflect.TypeFor[FetchParams](),
		"download":    reflect.TypeFor[DownloadParams](),
		"remote":      reflect.TypeFor[RemoteSupportParams](),
		"skill":       reflect.TypeFor[SkillManageParams](),
	}
	for name, typ := range types {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			generated := schema.Generate(typ)
			contract := schema.ValidationParameters(generated)
			withoutEnums(contract)

			data, err := json.Marshal(reflect.New(typ).Interface())
			require.NoError(t, err)
			var input map[string]any
			require.NoError(t, json.Unmarshal(data, &input))
			// A field marshalled with omitempty is absent; every required one is present.
			for _, field := range generated.Required {
				// A nil slice marshals to null, which no model sends for a list.
				if value, ok := input[field]; !ok || value == nil {
					input[field] = zeroFor(generated.Properties[field])
				}
			}
			require.NoError(t, schema.ValidateExact(contract, generated.Required, schema.RejectUnknown, input),
				"the type's own zero value must satisfy its contract: %s", data)
		})
	}
}

func zeroFor(s *schema.Schema) any {
	switch s.Type {
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		return []any{}
	default:
		return map[string]any{}
	}
}

type strictInput struct {
	Path string `json:"path" description:"file"`
}

// The unknown-field policy is chosen per tool and reaches validation through
// the tool's own description of its contract.
func TestUnknownFieldPolicyFollowsTheTool(t *testing.T) {
	t.Parallel()

	run := func(context.Context, strictInput, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("ok"), nil
	}
	lenient := fantasy.NewAgentTool("lenient", "x", run)
	strict := fantasy.RejectUnknownInput(fantasy.NewAgentTool("strict", "x", run))

	check := func(tool fantasy.AgentTool, input string) error {
		info := tool.Info()
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(input), &decoded))
		return schema.ValidateExact(info.Contract, info.Required, info.Unknown, decoded)
	}
	const misspelled = `{"path":"a.go","pth":"b.go"}`
	require.NoError(t, check(lenient, misspelled))
	require.ErrorContains(t, check(strict, misspelled), "pth")
	require.NoError(t, check(strict, `{"path":"a.go"}`))
}
