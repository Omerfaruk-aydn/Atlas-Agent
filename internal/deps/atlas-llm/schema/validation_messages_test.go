package schema

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestValidationNamesNestedField reproduces the failure shape of session
// aaae8f0a: a nested property failure surfaced only as "properties:
// Property {property} does not match the schema", with no field or reason.
func TestValidationNamesNestedField(t *testing.T) {
	t.Parallel()
	spec := Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"action": {Type: "string"},
			"advanced": {
				Type:       "object",
				Properties: map[string]*Schema{"x": {Type: "number"}},
			},
		},
	}
	err := ValidateAgainstSchema(map[string]any{
		"action":   "pointer_drag",
		"advanced": map[string]any{"x": "510"},
	}, spec)
	require.Error(t, err)
	msg := err.Error()
	require.NotContains(t, msg, "{property}")
	require.NotContains(t, msg, "{")
	require.Contains(t, msg, "/x")
	require.Contains(t, msg, "number")
}

// TestGenerateTimeIsString guards the root cause of the same session: a
// time.Time field was described as an object, so its encoded RFC 3339
// string failed validation on every browser workflow step.
func TestGenerateTimeIsString(t *testing.T) {
	t.Parallel()
	type request struct {
		NewerThan time.Time `json:"newer_than,omitempty"`
	}
	spec := Generate(reflect.TypeOf(request{}))
	require.Equal(t, "string", spec.Properties["newer_than"].Type)
	require.Equal(t, "date-time", spec.Properties["newer_than"].Format)
	require.NoError(t, ValidateAgainstSchema(map[string]any{"newer_than": time.Now().UTC().Format(time.RFC3339)}, spec))
}
