package agent

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

// Models often flatten the workflow call: they name the work action as the
// action ("action": "contract_add") or put the plan's requirements and
// stages next to the action instead of under "plan". Each of those failed
// with "unknown workflow action" or "plan is required" although the intent
// was plain, and the retries cost turns. normalizeWorkflowParams reads such
// calls the way they were meant; a properly nested call is unchanged.
func normalizeWorkflowParams(p WorkflowParams, raw string) WorkflowParams {
	// status and budget are workflow actions of their own as well.
	if p.WorkAction == "" && p.Action != "status" && p.Action != "budget" && slices.Contains(workActionNames(), p.Action) {
		p.WorkAction, p.Action = p.Action, "work"
	}
	if p.Action == "plan" && p.Plan == nil && raw != "" {
		var plan engineering.DeliveryPlan
		if json.Unmarshal([]byte(raw), &plan) == nil && (len(plan.Requirements) > 0 || len(plan.Stages) > 0) {
			p.Plan = &plan
		}
	}
	if p.Action == "design" && p.Design == nil && raw != "" {
		var design engineering.DesignBrief
		if json.Unmarshal([]byte(raw), &design) == nil && design.Audience != "" {
			p.Design = &design
		}
	}
	return p
}

func workActionNames() []string {
	var names []string
	for name := range strings.SplitSeq(workActions, ",") {
		names = append(names, strings.TrimSpace(name))
	}
	return names
}
