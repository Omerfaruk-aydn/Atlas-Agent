package csync

import (
	"encoding/json"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestMapSchemaAliasPreservesMapSchema(t *testing.T) {
	t.Parallel()
	m := NewMapFrom(map[string]int{"answer": 42})
	// Both forms must keep the alias because the reflector strips pointers.
	var alias interface{ JSONSchemaAlias() any } = Map[string, int]{}
	require.IsType(t, map[string]int{}, alias.JSONSchemaAlias())
	require.IsType(t, map[string]int{}, m.JSONSchemaAlias())
	schema := jsonschema.Reflect(m)
	require.NotEmpty(t, schema.ID, "the named Map retains its schema identity")
	schema.ID = ""
	got, err := json.Marshal(schema)
	require.NoError(t, err)
	want, err := json.Marshal(jsonschema.Reflect(map[string]int{}))
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(got))
	require.Equal(t, map[string]int{"answer": 42}, m.Copy())
}
