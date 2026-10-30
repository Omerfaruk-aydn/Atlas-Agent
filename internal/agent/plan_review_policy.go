package agent

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// planReviewRequired uses the same deterministic command-risk classifier as
// auto-approval. Plan prose is not guessed to be dangerous: a risky policy is
// triggered by concrete destructive, privileged, remote or system-changing
// verification commands. Required mode applies to every enabled review.
func planReviewRequired(policy string, plan *workplan.Plan) bool {
	if policy == "required" {
		return true
	}
	if policy != "risky" || plan == nil {
		return false
	}
	for _, feature := range plan.Features {
		if permission.ClassifyBashCommand(feature.Verify).Class == permission.ApprovalRisky {
			return true
		}
	}
	return false
}

func requiredPlanReviewFailure(reason string) (evaluatorDecision, bool) {
	return evaluatorDecision{Unverified: "Required plan review could not complete: " + reason}, true
}
