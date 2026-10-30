package schema

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// flexible decodes a string or an object itself, so reflection cannot know its
// wire format.
type flexible struct{ Raw string }

func (f *flexible) UnmarshalJSON(data []byte) error {
	f.Raw = string(data)
	return nil
}

type contractSample struct {
	Name    string            `json:"name" description:"name"`
	Payload json.RawMessage   `json:"payload" description:"any JSON"`
	Extra   any               `json:"extra,omitempty" description:"anything"`
	Mode    flexible          `json:"mode,omitempty" description:"string or object"`
	Counts  map[string]int    `json:"counts,omitempty" description:"counters"`
	Nested  []contractNested  `json:"nested,omitempty" description:"nested"`
	Labels  map[string]string `json:"labels,omitempty" description:"labels"`
}

type contractNested struct {
	ID    int `json:"id" description:"id"`
	Value any `json:"value,omitempty" description:"anything"`
}

func parameters(t *testing.T) (generated Schema, contract map[string]any) {
	t.Helper()
	generated = Generate(reflect.TypeFor[contractSample]())
	return generated, ValidationParameters(generated)
}

func validate(t *testing.T, input string, policy InputPolicy) error {
	t.Helper()
	generated, contract := parameters(t)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(input), &decoded))
	return ValidateExact(contract, generated.Required, policy, decoded)
}

// The advertised schema must not tell a model that a raw JSON field is an
// array of integers, which is how reflection sees a byte slice.
func TestRawMessageIsNotAdvertisedAsIntegers(t *testing.T) {
	t.Parallel()

	generated, _ := parameters(t)
	advertised := ToParameters(generated)["payload"].(map[string]any)
	require.NotContains(t, advertised, "items", "no integer array")
	require.NotContains(t, advertised, "type", "any JSON value is allowed")
	require.True(t, generated.Properties["payload"].Any)
	require.True(t, generated.Properties["extra"].Any)
	require.True(t, generated.Properties["mode"].Any, "a type that decodes itself is opaque")
	require.False(t, generated.Properties["name"].Any)
}

func TestExactContractAcceptsEveryShapeAnOpaqueFieldCanTake(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`{"name":"a","payload":{"k":1}}`,
		`{"name":"a","payload":[1,"two",null]}`,
		`{"name":"a","payload":"text"}`,
		`{"name":"a","payload":7,"extra":{"x":true},"mode":"fast"}`,
		`{"name":"a","payload":null,"extra":"s","mode":{"kind":"custom"}}`,
	} {
		require.NoError(t, validate(t, input, IgnoreUnknown), input)
	}
}

// What the schema can state exactly is still enforced, including values of
// maps and fields inside slices of objects.
func TestExactContractStillEnforcesWhatItCanState(t *testing.T) {
	t.Parallel()

	for input, field := range map[string]string{
		`{"payload":1}`:          "name",
		`{"name":5,"payload":1}`: "name",
		`{"name":"a","payload":1,"counts":{"x":"many"}}`: "x",
		`{"name":"a","payload":1,"labels":{"x":1}}`:      "x",
		`{"name":"a","payload":1,"nested":[{"id":"7"}]}`: "id",
		`{"name":"a","payload":1,"nested":[{}]}`:         "id",
	} {
		err := validate(t, input, IgnoreUnknown)
		require.Error(t, err, input)
		require.Contains(t, err.Error(), field, input)
	}
	require.NoError(t, validate(t, `{"name":"a","payload":1,"counts":{"x":2},"labels":{"y":"z"},"nested":[{"id":3,"value":"any"}]}`, IgnoreUnknown))
}

// Unknown fields are a decision of the tool's contract, stated per tool.
func TestUnknownFieldPolicyIsExplicit(t *testing.T) {
	t.Parallel()

	input := `{"name":"a","payload":1,"replace_al":true}`
	require.NoError(t, validate(t, input, IgnoreUnknown), "default: extras are ignored")
	err := validate(t, input, RejectUnknown)
	require.Error(t, err)
	require.Contains(t, err.Error(), "replace_al", "the unknown field is named")

	// Rejection covers the top level only: opaque and map values may hold any keys.
	require.NoError(t, validate(t, `{"name":"a","payload":{"anything":1},"counts":{"free":1}}`, RejectUnknown))
}

// Providers advertise the generated schema; the change must not alter the
// shape of fields the generator could always describe.
func TestAdvertisedShapeOfDescribableFieldsIsUnchanged(t *testing.T) {
	t.Parallel()

	generated, _ := parameters(t)
	advertised := ToParameters(generated)
	require.Equal(t, "string", advertised["name"].(map[string]any)["type"])
	require.Equal(t, "object", advertised["extra"].(map[string]any)["type"], "interface keeps its advertised hint")
	require.Equal(t, "array", advertised["nested"].(map[string]any)["type"])
	require.ElementsMatch(t, []string{"name", "payload"}, generated.Required)
}
