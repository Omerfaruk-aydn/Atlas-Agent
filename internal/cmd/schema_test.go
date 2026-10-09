package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestPublishedSchemaIncludesCurrentDeviceSettings(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../schema.json")
	require.NoError(t, err)
	var published, current map[string]any
	require.NoError(t, json.Unmarshal(data, &published))
	data, err = json.Marshal(new(jsonschema.Reflector).Reflect(&config.Config{}))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &current))
	want := current["$defs"].(map[string]any)
	got := published["$defs"].(map[string]any)
	for _, name := range []string{"Tools", "ToolDevices"} {
		require.Equal(t, want[name], got[name], "published %s schema drifted from configuration", name)
	}
}

func TestSchemaNoBrokenRefs(t *testing.T) {
	t.Parallel()

	reflector := new(jsonschema.Reflector)
	bts, err := json.Marshal(reflector.Reflect(&config.Config{}))
	require.NoError(t, err)

	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(bts, &schema))
	require.NotEmpty(t, schema.Defs, "schema should have definitions")

	for name := range schema.Defs {
		require.NotContains(t, name, "/", "schema $def key %q contains '/' which breaks JSON Pointer $ref resolution", name)
	}
}

func TestSchemaProvidersHasAdditionalProperties(t *testing.T) {
	t.Parallel()

	reflector := new(jsonschema.Reflector)
	bts, err := json.Marshal(reflector.Reflect(&config.Config{}))
	require.NoError(t, err)

	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(bts, &schema))

	var cfg struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(schema.Defs["Config"], &cfg))

	providersRaw, ok := cfg.Properties["providers"]
	require.True(t, ok, "Config should have a providers property")

	var providers struct {
		Type                 string          `json:"type"`
		AdditionalProperties json.RawMessage `json:"additionalProperties"`
	}
	require.NoError(t, json.Unmarshal(providersRaw, &providers))
	require.Equal(t, "object", providers.Type)
	require.True(t, strings.Contains(string(providers.AdditionalProperties), "ProviderConfig"),
		"providers should use additionalProperties with a ProviderConfig ref, got: %s", string(providers.AdditionalProperties))
}
