package evaluation

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Error kinds separate infrastructure failures from model behaviour, so a
// rate limit is never counted as the model failing the task.
const (
	ErrorProvider = "provider"
	ErrorTimeout  = "timeout"
	ErrorAgent    = "agent"
)

var providerFailure = regexp.MustCompile(`(?i)rate.?limit|\b429\b|\b5\d\d\b|overloaded|unavailable|quota|unauthori[sz]ed|forbidden|\b401\b|\b402\b|\b403\b|payment required|insufficient.balance|not found for account|connection (reset|refused)|no such host|tls|eof|transport|timed out waiting for header`)

// ClassifyError returns the kind of an agent run failure.
func ClassifyError(agentError, finishError string) string {
	text := agentError + " " + finishError
	switch {
	case strings.TrimSpace(text) == "":
		return ""
	case strings.Contains(strings.ToLower(text), "deadline exceeded") || strings.Contains(strings.ToLower(text), "context deadline"):
		return ErrorTimeout
	case providerFailure.MatchString(text):
		return ErrorProvider
	default:
		return ErrorAgent
	}
}

// AttemptGroup summarizes the attempts of one model on one scenario.
type AttemptGroup struct {
	DeliveryObserved    bool   `json:"delivery_observed"`
	DeliveredPassed     *bool  `json:"delivered_passed,omitempty"`
	DeliveredWorkspace  string `json:"delivered_workspace,omitempty"`
	ActualSelectionLoss bool   `json:"actual_selection_loss"`
	Model               string `json:"model"`
	Scenario            string `json:"scenario"`
	Attempts            int    `json:"attempts"`
	Valid               int    `json:"valid_attempts"`
	FirstPassed         bool   `json:"first_passed"`
	AnyPassed           bool   `json:"any_passed"`
	SelectedAttempt     int    `json:"predicted_selected_attempt"`
	SelectedPassed      bool   `json:"predicted_selected_passed"`
	SelectionBasis      string `json:"selection_basis"`
	SelectionLoss       bool   `json:"selection_loss"`
}

// AttemptReport separates single-attempt success, success in any attempt
// and the success of the attempt Atlas itself would deliver.
type AttemptReport struct {
	ProviderInvocations    *int64         `json:"provider_invocations,omitempty"`
	InvocationMeasuredRuns int            `json:"invocation_measured_runs"`
	DeliveredRate          *float64       `json:"delivered_success_rate,omitempty"`
	DeliveredGroups        int            `json:"delivered_groups"`
	ActualSelectionLosses  int            `json:"actual_selection_losses"`
	EvidenceBasis          string         `json:"evidence_basis"`
	Groups                 []AttemptGroup `json:"groups"`
	PassAt1                float64        `json:"pass_at_1"`
	PassAtK                float64        `json:"pass_at_k"`
	SelectedRate           float64        `json:"predicted_selection_success_rate"`
	SelectionLosses        int            `json:"selection_losses"`
	FalseCompletions       int            `json:"false_completions"`
	ProviderErrors         int            `json:"provider_errors"`
	Timeouts               int            `json:"timeouts"`
	AgentErrors            int            `json:"agent_errors"`
	StoppedForUser         int            `json:"stopped_for_user"`
	ToolCalls              int64          `json:"tool_calls"`
	ToolErrors             int64          `json:"tool_errors"`
	RepeatedToolCalls      int64          `json:"repeated_tool_calls"`
	ProviderSteps          int64          `json:"provider_steps"`
	Tokens                 int64          `json:"tokens"`
	DurationMS             int64          `json:"duration_ms"`
	Recovered              int            `json:"recovered_after_tool_error"`
}

func passed(r LiveRecord) bool {
	if r.ExecutionError != "" || r.ErrorKind != "" || r.FinishError != "" || r.FinishReason == "error" || len(r.Checks) == 0 {
		return false
	}
	for _, c := range r.Checks {
		if !c.Passed || strings.TrimSpace(c.Evidence) == "" {
			return false
		}
	}
	return true
}

// selectAttempt picks what Atlas would deliver using only its own runtime
// evidence, never the external checkers: the first attempt with an observed
// passing check and no open requirement, else the first that claimed
// completion, else the first attempt.
func selectAttempt(group []LiveRecord) (int, string) {
	for i, r := range group {
		if r.AtlasVerified && r.ErrorKind == "" && r.ExecutionError == "" && r.FinishError == "" {
			return i, "first attempt with runtime-observed verification"
		}
	}
	for i, r := range group {
		if r.ClaimedComplete && r.ErrorKind == "" {
			return i, "first attempt that claimed completion (no runtime verification observed)"
		}
	}
	for i, r := range group {
		if r.ErrorKind != ErrorProvider {
			return i, "no attempt verified or claimed completion; first non-provider-failed attempt"
		}
	}
	return 0, "all attempts failed at the provider"
}

// SummarizeAttempts groups live records by model and scenario. Records of
// one scenario must come from the same case definition: acceptance criteria
// cannot change after results are seen.
func SummarizeAttempts(records []LiveRecord) (AttemptReport, error) {
	report := AttemptReport{EvidenceBasis: "external checkers decide success; predicted selection is a retrospective heuristic, not an observed Atlas selection; delivered_success_rate uses only separately executed Atlas delivery runs; absent delivery records mean actual selection was not measured; pass_at_1 uses the first non-provider-failed attempt"}
	type key struct{ model, scenario string }
	groups := map[key][]LiveRecord{}
	deliveries := map[key]LiveRecord{}
	hashes := map[string]string{}
	var keys []key
	for _, r := range records {
		if r.ProviderInvocations != nil {
			if *r.ProviderInvocations < 0 {
				return AttemptReport{}, errors.New("negative provider invocation count")
			}
			if report.ProviderInvocations == nil {
				n := int64(0)
				report.ProviderInvocations = &n
			}
			*report.ProviderInvocations += *r.ProviderInvocations
			report.InvocationMeasuredRuns++
		}
		if r.CaseHash != "" {
			if prev, ok := hashes[r.Scenario]; ok && prev != r.CaseHash {
				return AttemptReport{}, fmt.Errorf("scenario %q mixes different case definitions; criteria cannot change between attempts", r.Scenario)
			}
			hashes[r.Scenario] = r.CaseHash
		}
		k := key{r.Model, r.Scenario}
		if r.Delivery {
			if _, exists := deliveries[k]; exists {
				return AttemptReport{}, errors.New("duplicate delivered run for model/scenario")
			}
			deliveries[k] = r
			continue
		}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], r)
	}
	if len(keys) == 0 {
		return AttemptReport{}, errors.New("no records")
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		return keys[i].scenario < keys[j].scenario
	})
	var first, any, selected, scored int
	deliveredPassed := 0
	for _, k := range keys {
		group := groups[k]
		sort.SliceStable(group, func(i, j int) bool { return group[i].Attempt < group[j].Attempt })
		g := AttemptGroup{Model: k.model, Scenario: k.scenario, Attempts: len(group)}
		seenAttempts := map[int]bool{}
		for _, r := range group {
			if r.Attempt < 0 || seenAttempts[r.Attempt] {
				return AttemptReport{}, fmt.Errorf("model %q scenario %q has an invalid or duplicate attempt %d", k.model, k.scenario, r.Attempt)
			}
			seenAttempts[r.Attempt] = true
			switch r.ErrorKind {
			case ErrorProvider:
				report.ProviderErrors++
			case ErrorTimeout:
				report.Timeouts++
			case ErrorAgent:
				report.AgentErrors++
			}
			if r.FinishReason == "tool_use" || r.FinishReason == "canceled" {
				report.StoppedForUser++
			}
			ok := passed(r)
			if r.ErrorKind != ErrorProvider {
				if g.Valid == 0 {
					g.FirstPassed = ok
				}
				g.Valid++
			}
			g.AnyPassed = g.AnyPassed || ok
			if r.ClaimedComplete && !ok {
				report.FalseCompletions++
			}
			if r.ToolErrors > 0 && ok {
				report.Recovered++
			}
			report.ToolCalls += r.ToolCalls
			report.ToolErrors += r.ToolErrors
			report.RepeatedToolCalls += r.RepeatedToolCalls
			report.ProviderSteps += r.ProviderSteps
			report.Tokens += r.Tokens
			report.DurationMS += r.DurationMS
		}
		index, basis := selectAttempt(group)
		g.SelectedAttempt, g.SelectionBasis = group[index].Attempt, basis
		g.SelectedPassed = passed(group[index])
		g.SelectionLoss = g.AnyPassed && !g.SelectedPassed
		if delivery, ok := deliveries[k]; ok {
			if delivery.CaseHash == "" || len(delivery.CandidateAttempts) == 0 {
				return AttemptReport{}, errors.New("delivered run requires case hash and candidate attempt provenance")
			}
			seen := map[int]bool{}
			for _, attempt := range delivery.CandidateAttempts {
				if !seenAttempts[attempt] || seen[attempt] {
					return AttemptReport{}, errors.New("delivered run references unknown or duplicate candidates")
				}
				seen[attempt] = true
			}
			if len(seen) != len(group) {
				return AttemptReport{}, errors.New("delivered run must account for all candidate attempts")
			}
			g.DeliveryObserved = true
			g.DeliveredWorkspace = delivery.Workspace
			if delivery.ErrorKind != ErrorProvider {
				ok := passed(delivery)
				g.DeliveredPassed = &ok
				report.DeliveredGroups++
				if ok {
					deliveredPassed++
				}
				g.ActualSelectionLoss = g.AnyPassed && !ok
				if g.ActualSelectionLoss {
					report.ActualSelectionLosses++
				}
			} else {
				report.ProviderErrors++
			}
			report.ToolCalls += delivery.ToolCalls
			report.ToolErrors += delivery.ToolErrors
			report.ProviderSteps += delivery.ProviderSteps
			report.Tokens += delivery.Tokens
			report.DurationMS += delivery.DurationMS
		}
		if g.SelectionLoss {
			report.SelectionLosses++
		}
		// A group whose every attempt hit a provider failure says nothing
		// about the model and is excluded from the rates.
		if g.Valid > 0 {
			scored++
			if g.FirstPassed {
				first++
			}
			if g.AnyPassed {
				any++
			}
			if g.SelectedPassed {
				selected++
			}
		}
		report.Groups = append(report.Groups, g)
	}
	if len(deliveries) > len(keys) {
		return AttemptReport{}, errors.New("delivery has no candidate group")
	}
	for k := range deliveries {
		if _, ok := groups[k]; !ok {
			return AttemptReport{}, errors.New("delivery has no candidate group")
		}
	}
	if report.DeliveredGroups > 0 {
		rate := float64(deliveredPassed) / float64(report.DeliveredGroups)
		report.DeliveredRate = &rate
	}
	if scored > 0 {
		report.PassAt1 = float64(first) / float64(scored)
		report.PassAtK = float64(any) / float64(scored)
		report.SelectedRate = float64(selected) / float64(scored)
	}
	return report, nil
}
