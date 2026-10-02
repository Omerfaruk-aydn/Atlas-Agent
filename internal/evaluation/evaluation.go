// Package evaluation scores recorded agent runs against explicit criteria.
// It does not execute agents or independently authenticate submitted evidence.
package evaluation

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

//go:embed scenarios.json
var scenarioData []byte

type Scenario struct {
	ID       string   `json:"id"`
	Prompt   string   `json:"prompt"`
	Criteria []string `json:"criteria"`
}

type Check struct {
	Criterion string `json:"criterion"`
	Passed    bool   `json:"passed"`
	Evidence  string `json:"evidence"`
}

type Run struct {
	Role              string  `json:"role,omitempty"`
	ExecutionError    string  `json:"execution_error,omitempty"`
	Cost              float64 `json:"cost_usd,omitempty"`
	ClaimObserved     *bool   `json:"claim_observed,omitempty"`
	Scenario          string  `json:"scenario"`
	Model             string  `json:"model,omitempty"`
	PromptVersion     string  `json:"prompt_version,omitempty"`
	ClaimedComplete   bool    `json:"claimed_complete"`
	Checks            []Check `json:"checks"`
	ToolCalls         int64   `json:"tool_calls"`
	RepeatedToolCalls int64   `json:"repeated_tool_calls"`
	Tokens            int64   `json:"tokens"`
	DurationMS        int64   `json:"duration_ms"`
}

type Report struct {
	ExecutionFailures int      `json:"execution_failures,omitempty"`
	Cost              float64  `json:"cost_usd,omitempty"`
	UnobservedClaims  int      `json:"unobserved_claims,omitempty"`
	EvidenceBasis     string   `json:"evidence_basis"`
	Scenarios         int      `json:"scenarios"`
	Evaluated         int      `json:"evaluated"`
	Successful        int      `json:"successful"`
	FalseCompletions  int      `json:"false_completions"`
	AcceptanceRate    float64  `json:"acceptance_rate"`
	RepeatedCallRate  float64  `json:"repeated_call_rate"`
	Tokens            int64    `json:"tokens"`
	DurationMS        int64    `json:"duration_ms"`
	MissingScenarios  []string `json:"missing_scenarios"`
}

func Scenarios() ([]Scenario, error) {
	var scenarios []Scenario
	err := json.Unmarshal(scenarioData, &scenarios)
	return scenarios, err
}

// Score validates reports and treats missing checks or evidence as unmet criteria.
func Score(scenarios []Scenario, runs []Run) (Report, error) {
	report := Report{EvidenceBasis: "submitted run records; evidence is not independently authenticated", Scenarios: len(scenarios), MissingScenarios: []string{}}
	index := make(map[string]Scenario, len(scenarios))
	for _, scenario := range scenarios {
		if _, exists := index[scenario.ID]; exists || scenario.ID == "" || len(scenario.Criteria) == 0 {
			return Report{}, fmt.Errorf("invalid or duplicate scenario %q", scenario.ID)
		}
		criteria := map[string]bool{}
		for _, criterion := range scenario.Criteria {
			if strings.TrimSpace(criterion) == "" || criteria[criterion] {
				return Report{}, fmt.Errorf("invalid or duplicate criterion in %q", scenario.ID)
			}
			criteria[criterion] = true
		}
		index[scenario.ID] = scenario
	}
	seen := map[string]bool{}
	var calls, repeated, accepted, total int64
	for _, run := range runs {
		scenario, ok := index[run.Scenario]
		if !ok || seen[run.Scenario] {
			return Report{}, fmt.Errorf("unknown or duplicate scenario %q", run.Scenario)
		}
		seen[run.Scenario] = true
		if run.ToolCalls < 0 || run.RepeatedToolCalls < 0 || run.Tokens < 0 || run.DurationMS < 0 || run.RepeatedToolCalls > run.ToolCalls || run.Cost < 0 || math.IsNaN(run.Cost) || math.IsInf(run.Cost, 0) {
			return Report{}, fmt.Errorf("invalid metrics for %q", run.Scenario)
		}
		allowed := map[string]bool{}
		for _, criterion := range scenario.Criteria {
			allowed[criterion] = true
		}
		checks := map[string]Check{}
		for _, check := range run.Checks {
			if _, exists := checks[check.Criterion]; exists || !allowed[check.Criterion] {
				return Report{}, fmt.Errorf("unknown or duplicate check %q", check.Criterion)
			}
			checks[check.Criterion] = check
		}
		passed := run.ExecutionError == ""
		if !passed {
			report.ExecutionFailures++
		}
		for _, criterion := range scenario.Criteria {
			total++
			check := checks[criterion]
			if run.ExecutionError == "" && check.Passed && strings.TrimSpace(check.Evidence) != "" {
				accepted++
			} else {
				passed = false
			}
		}
		report.Evaluated++
		if passed {
			report.Successful++
		} else if run.ClaimedComplete {
			report.FalseCompletions++
		}
		calls += run.ToolCalls
		repeated += run.RepeatedToolCalls
		report.Tokens += run.Tokens
		report.DurationMS += run.DurationMS
		report.Cost += run.Cost
		if run.ClaimObserved != nil && !*run.ClaimObserved {
			report.UnobservedClaims++
		}
	}
	for _, scenario := range scenarios {
		if !seen[scenario.ID] {
			report.MissingScenarios = append(report.MissingScenarios, scenario.ID)
		}
	}
	if total > 0 {
		report.AcceptanceRate = float64(accepted) / float64(total)
	}
	if calls > 0 {
		report.RepeatedCallRate = float64(repeated) / float64(calls)
	}
	return report, nil
}

type Comparison struct {
	Role          string `json:"role,omitempty"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	Report        Report `json:"report"`
}

// Compare scores each model/prompt variant separately without mixing scenarios.
func Compare(scenarios []Scenario, runs []Run) ([]Comparison, error) {
	type key struct{ model, version, role string }
	groups := map[key][]Run{}
	var keys []key
	for _, run := range runs {
		k := key{run.Model, run.PromptVersion, run.Role}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], run)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].role != keys[j].role {
			return keys[i].role < keys[j].role
		}
		if keys[i].model == keys[j].model {
			return keys[i].version < keys[j].version
		}
		return keys[i].model < keys[j].model
	})
	var out []Comparison
	for _, k := range keys {
		report, err := scoreSamples(scenarios, groups[k])
		if err != nil {
			return nil, err
		}
		out = append(out, Comparison{Model: k.model, PromptVersion: k.version, Role: k.role, Report: report})
	}
	return out, nil
}

// scoreSamples aggregates repeated evaluations without mixing criteria or roles.
func scoreSamples(scenarios []Scenario, runs []Run) (Report, error) {
	base, err := Score(scenarios, nil)
	if err != nil {
		return Report{}, err
	}
	known := map[string]Scenario{}
	for _, s := range scenarios {
		known[s.ID] = s
	}
	seen := map[string]bool{}
	var calls, repeated int64
	var accepted, total float64
	for _, run := range runs {
		s, ok := known[run.Scenario]
		if !ok {
			return Report{}, fmt.Errorf("unknown scenario %s", run.Scenario)
		}
		r, err := Score([]Scenario{s}, []Run{run})
		if err != nil {
			return Report{}, err
		}
		seen[run.Scenario] = true
		base.Evaluated += r.Evaluated
		base.Successful += r.Successful
		base.FalseCompletions += r.FalseCompletions
		base.ExecutionFailures += r.ExecutionFailures
		base.UnobservedClaims += r.UnobservedClaims
		base.Tokens += r.Tokens
		base.DurationMS += r.DurationMS
		base.Cost += r.Cost
		calls += run.ToolCalls
		repeated += run.RepeatedToolCalls
		accepted += r.AcceptanceRate * float64(len(s.Criteria))
		total += float64(len(s.Criteria))
	}
	base.MissingScenarios = []string{}
	for _, s := range scenarios {
		if !seen[s.ID] {
			base.MissingScenarios = append(base.MissingScenarios, s.ID)
		}
	}
	if total > 0 {
		base.AcceptanceRate = accepted / total
	}
	if calls > 0 {
		base.RepeatedCallRate = float64(repeated) / float64(calls)
	}
	return base, nil
}
