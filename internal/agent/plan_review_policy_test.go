package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
	"github.com/stretchr/testify/require"
)

func TestPlanReviewRequiredPolicy(t *testing.T) {
	t.Parallel()
	benign := &workplan.Plan{Features: []workplan.Feature{{Verify: "go test ./..."}}}
	risky := &workplan.Plan{Features: []workplan.Feature{{Verify: "go test ./..."}, {Verify: "kubectl delete deployment api"}}}
	require.False(t, planReviewRequired("optional", risky))
	require.True(t, planReviewRequired("required", benign))
	require.False(t, planReviewRequired("risky", benign))
	require.True(t, planReviewRequired("risky", risky))
	require.False(t, planReviewRequired("risky", nil))
}

func TestRequiredPlanReviewFailureIsUnverified(t *testing.T) {
	t.Parallel()
	decision, stop := requiredPlanReviewFailure("reviewer is unavailable")
	require.True(t, stop)
	require.Empty(t, decision.Continue)
	require.Contains(t, decision.Unverified, "reviewer is unavailable")
}
