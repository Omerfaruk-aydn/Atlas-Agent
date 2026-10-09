package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type compactLeaf struct {
	Mode   string `json:"mode" enum:"fast,slow" description:"Execution mode with a deliberately long explanation for the documented first occurrence of this shape."`
	Target string `json:"target" description:"Target identifier with another long explanation so the repeated copy is worth compacting."`
	Limit  int    `json:"limit,omitempty" description:"Optional bound that keeps its integer type in every copy."`
}

type compactRoot struct {
	First  compactLeaf   `json:"first" description:"Primary input."`
	Many   []compactLeaf `json:"many,omitempty" description:"Repeated inputs."`
	Nested struct {
		Again compactLeaf `json:"again"`
	} `json:"nested"`
}

func structure(node map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range node {
		switch key {
		case "description":
			continue
		case "properties":
			props := map[string]any{}
			for name, child := range value.(map[string]any) {
				props[name] = structure(child.(map[string]any))
			}
			out[key] = props
		case "items":
			out[key] = structure(value.(map[string]any))
		default:
			out[key] = value
		}
	}
	return out
}

func TestCompactRepeatedKeepsContractAndDocumentsFirstOccurrence(t *testing.T) {
	t.Parallel()
	original := Generate(reflect.TypeFor[compactRoot]())
	compacted := Generate(reflect.TypeFor[compactRoot]())
	CompactRepeated(&compacted)

	// Types, enums, required lists and nesting are identical; only
	// descriptions differ.
	require.Equal(t, structure(ToMap(original)), structure(ToMap(compacted)))

	first := compacted.Properties["first"]
	require.Contains(t, first.Properties["mode"].Description, "Execution mode")
	require.Equal(t, "Primary input.", first.Description)

	many := compacted.Properties["many"]
	require.Equal(t, "Repeated inputs.", many.Description)
	require.Equal(t, "Same fields and constraints as first.", many.Items.Description)
	require.Empty(t, many.Items.Properties["mode"].Description)
	require.Equal(t, []any{"fast", "slow"}, many.Items.Properties["mode"].Enum)
	require.Equal(t, []string{"mode", "target"}, many.Items.Required)

	again := compacted.Properties["nested"].Properties["again"]
	require.Equal(t, "Same fields and constraints as first.", again.Description)

	before, _ := json.Marshal(ToMap(original))
	after, _ := json.Marshal(ToMap(compacted))
	require.Less(t, len(after), len(before))

	// Compaction is deterministic.
	again2 := Generate(reflect.TypeFor[compactRoot]())
	CompactRepeated(&again2)
	require.Equal(t, ToMap(compacted), ToMap(again2))
}

func TestCompactRepeatedLeavesSmallShapesDocumented(t *testing.T) {
	t.Parallel()
	type small struct {
		A string `json:"a" description:"short"`
	}
	type root struct {
		X small `json:"x"`
		Y small `json:"y"`
	}
	s := Generate(reflect.TypeFor[root]())
	CompactRepeated(&s)
	require.Equal(t, "short", s.Properties["y"].Properties["a"].Description)
}
