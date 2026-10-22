package agent

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func req(id string, phase outcome.Phase, deps ...string) outcome.Requirement {
	return outcome.Requirement{ID: id, Phase: phase, DependsOn: deps}
}

func TestDecideCompletion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   unifiedInput
		want completionVerdict
	}{
		{"all verified", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified), req("b", outcome.PhaseDelivered)}}, completeVerdict},
		{"nothing planned stays complete", unifiedInput{}, completeVerdict},
		{"unstarted work is incomplete", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhasePlanned)}}, incompleteVerdict},
		{"applied is not verified", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied)}}, unverifiedVerdict},
		{"green contract supports applied work", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied)}, ContractGreen: true}, completeVerdict},
		{"explicit requirement still needs its own proof", unifiedInput{Requirements: []outcome.Requirement{req("work:R1", outcome.PhaseApplied)}, ContractGreen: true}, unverifiedVerdict},
		{"green contract cannot hide unstarted work", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied), req("b", outcome.PhasePlanned)}, ContractGreen: true}, incompleteVerdict},
		{"unknown effect blocks", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, UnknownEffects: 1}, blockedVerdict},
		{"paused blocks", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Paused: true}, blockedVerdict},
		{"blocked requirement blocks", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseBlocked)}}, blockedVerdict},
		{"open finding", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, OpenFindings: []string{"nil map write"}}, incompleteVerdict},
		{"missing required check", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, ContractGaps: []string{"go test: not run"}}, incompleteVerdict},
		{"review failed", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Review: reviewFailed}, incompleteVerdict},
		{"review could not run", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Review: reviewUnavailable}, unverifiedVerdict},
		{"review gave a bad report", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Review: reviewInvalidReport}, unverifiedVerdict},
		{"review passed", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Review: reviewPassed}, completeVerdict},
		{"waived review is stated, not hidden", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseVerified)}, Review: reviewWaived}, completeVerdict},
		{"dependent waits on unverified upstream", unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied), req("b", outcome.PhaseVerified, "a")}}, unverifiedVerdict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideCompletion(tc.in)
			require.Equal(t, tc.want, got.Verdict, "%v", got.Reasons)
			if got.Verdict != completeVerdict {
				require.NotEmpty(t, got.Reasons, "a non-complete verdict must say why")
			}
		})
	}
}

func TestDecisionKeepsTheContractBasisVisible(t *testing.T) {
	t.Parallel()
	got := decideCompletion(unifiedInput{Requirements: []outcome.Requirement{req("a", outcome.PhaseApplied)}, ContractGreen: true})
	require.True(t, got.Complete())
	require.Equal(t, []string{"a"}, got.ByContract)
	require.Contains(t, got.Reasons[0], "project contract")
}

// The legacy paths accept an unreviewable turn as finished; the unified
// decision does not. This is the divergence shadow mode exists to surface.
func TestShadowSurfacesWhereLegacyOverclaims(t *testing.T) {
	t.Parallel()

	requirements := []outcome.Requirement{req("a", outcome.PhaseVerified)}
	evaluation := evaluatorDecision{Review: reviewUnavailable, Unverified: "reviewer timed out"}

	require.True(t, legacyComplete(evaluation, requirements), "legacy ends the turn as if nothing were wrong")
	got := decideCompletion(unifiedInput{Requirements: requirements, Review: evaluation.Review})
	require.Equal(t, unverifiedVerdict, got.Verdict)
	require.False(t, got.Complete())

	// Where they should agree, they do.
	require.False(t, legacyComplete(evaluatorDecision{Continue: "fix it"}, requirements))
	require.True(t, legacyComplete(evaluatorDecision{Review: reviewPassed}, requirements))
}
