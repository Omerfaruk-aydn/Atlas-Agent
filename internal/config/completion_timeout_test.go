package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompletionTimeoutIsBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		harness *Harness
		want    time.Duration
	}{
		{name: "nil uses default", want: 2 * time.Minute},
		{name: "zero uses default", harness: &Harness{}, want: 2 * time.Minute},
		{name: "negative uses default", harness: &Harness{CompletionTimeoutSeconds: -1}, want: 2 * time.Minute},
		{name: "configured", harness: &Harness{CompletionTimeoutSeconds: 5}, want: 5 * time.Second},
		{name: "capped", harness: &Harness{CompletionTimeoutSeconds: 601}, want: 10 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.harness.CompletionTimeout())
		})
	}
}

func TestPlanReviewPolicyValidation(t *testing.T) {
	t.Parallel()
	require.Equal(t, "optional", (*WorkPlan)(nil).PlanReviewPolicy())
	require.Equal(t, "optional", (&WorkPlan{}).PlanReviewPolicy())
	require.Equal(t, "required", (&WorkPlan{ReviewPolicy: "required"}).PlanReviewPolicy())
	require.Equal(t, "risky", (&WorkPlan{ReviewPolicy: "invalid"}).PlanReviewPolicy())
}
