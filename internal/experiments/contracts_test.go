package experiments

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContractComparisonDetectsRemovedOperationAndRequiredChange(t *testing.T) {
	t.Parallel()
	before := []byte(`{"openapi":"3.1.0","paths":{"/users":{"get":{"description":"old","responses":{"200":{}}}}},"components":{"schemas":{"User":{"required":["name"]}}}}`)
	after := []byte(`{"openapi":"3.1.0","paths":{"/users":{}},"components":{"schemas":{"User":{"required":["name","email"]}}}}`)
	result, err := CompareOpenAPI(t.Context(), before, after)
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.Len(t, result.Changes, 2)
	require.Equal(t, "/paths/~1users/get", result.Changes[0].Path)
	require.Equal(t, "removed", result.Changes[0].Kind)
}
