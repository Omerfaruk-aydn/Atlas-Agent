package evaluation

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

type SelectionPolicy struct {
	Role              string   `json:"role"`
	Scenarios         []string `json:"scenarios"`
	Candidates        []string `json:"candidates"`
	PromptVersion     string   `json:"prompt_version"`
	MinSamples        int      `json:"min_samples,omitempty"`
	MinSuccessRate    float64  `json:"min_success_rate,omitempty"`
	MaxMeanCost       float64  `json:"max_mean_cost_usd,omitempty"`
	MaxMeanDurationMS int64    `json:"max_mean_duration_ms,omitempty"`
	MaxAgeHours       int      `json:"max_age_hours,omitempty"`
}

type ModelRecommendation struct {
	Role           string  `json:"role"`
	Model          string  `json:"model"`
	Samples        int     `json:"samples"`
	SuccessRate    float64 `json:"success_rate"`
	MeanCost       float64 `json:"mean_cost_usd"`
	MeanDurationMS float64 `json:"mean_duration_ms"`
	EvidenceBasis  string  `json:"evidence_basis"`
}

func ReadJSONFile(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 1024*1024 {
		return fmt.Errorf("JSON file exceeds 1 MiB")
	}
	return json.Unmarshal(data, target)
}

// Recommend compares the same scenarios and prompt version across every candidate.
// It accepts only role-tagged live records with runtime ledger metrics and dates.
func Recommend(policy SelectionPolicy, records []LiveRecord, now time.Time) (ModelRecommendation, error) {
	if policy.Role == "" || policy.PromptVersion == "" || len(policy.Scenarios) == 0 || len(policy.Scenarios) > 9 || len(policy.Candidates) == 0 || len(policy.Candidates) > 8 || policy.MinSamples < 0 || policy.MinSamples > 100 || policy.MaxAgeHours < 0 || policy.MaxAgeHours > 8760 || policy.MaxMeanDurationMS < 0 {
		return ModelRecommendation{}, fmt.Errorf("invalid role selection policy")
	}
	for _, n := range []float64{policy.MinSuccessRate, policy.MaxMeanCost} {
		if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return ModelRecommendation{}, fmt.Errorf("invalid selection thresholds")
		}
	}
	if policy.MinSuccessRate > 1 {
		return ModelRecommendation{}, fmt.Errorf("success rate must be between zero and one")
	}
	minimum := policy.MinSamples
	if minimum == 0 {
		minimum = 3
	}
	age := policy.MaxAgeHours
	if age == 0 {
		age = 168
	}
	threshold := policy.MinSuccessRate
	if threshold == 0 {
		threshold = 0.8
	}
	scenarios, err := Scenarios()
	if err != nil {
		return ModelRecommendation{}, err
	}
	known := map[string]Scenario{}
	for _, s := range scenarios {
		known[s.ID] = s
	}
	selected := map[string]Scenario{}
	for _, id := range policy.Scenarios {
		s, ok := known[id]
		if !ok {
			return ModelRecommendation{}, fmt.Errorf("unknown scenario %s", id)
		}
		if _, exists := selected[id]; exists {
			return ModelRecommendation{}, fmt.Errorf("duplicate scenario")
		}
		selected[id] = s
	}
	seen := map[string]bool{}
	seenRuns := map[string]bool{}
	fixtures := map[string]string{}
	var ranked []ModelRecommendation
	for _, model := range policy.Candidates {
		provider, id, explicit := strings.Cut(model, "/")
		if seen[model] || !explicit || provider == "" || id == "" || strings.TrimSpace(model) != model || len(model) > 512 || strings.HasPrefix(model, "-") {
			return ModelRecommendation{}, fmt.Errorf("candidates must be unique provider/model IDs")
		}
		seen[model] = true
		counts := map[string]int{}
		successes := 0
		candidate := ModelRecommendation{Role: policy.Role, Model: model, EvidenceBasis: "role-tagged live records with external checkers and cumulative runtime ledger; local records are not cryptographically authenticated"}
		for _, record := range records {
			scenario, ok := selected[record.Scenario]
			if !ok || record.Model != model || record.Role != policy.Role || record.PromptVersion != policy.PromptVersion {
				continue
			}
			if record.RecordedAt <= 0 || record.RecordedAt > now.Unix() || now.Sub(time.Unix(record.RecordedAt, 0)) > time.Duration(age)*time.Hour {
				continue
			}
			if record.MetricsSource != "cumulative_runtime_ledger" || record.SessionID == "" || record.Workspace == "" || record.Baseline == "" || record.FixtureHash == "" {
				return ModelRecommendation{}, fmt.Errorf("matching live record lacks cumulative runtime metrics or identity")
			}
			fixture := record.Baseline + "\x00" + record.FixtureHash
			if previous, ok := fixtures[record.Scenario]; ok && previous != fixture {
				return ModelRecommendation{}, fmt.Errorf("scenario %s has incomparable repository baselines or task/checker fixtures", record.Scenario)
			}
			fixtures[record.Scenario] = fixture
			identity := model + "\x00" + record.Workspace + "\x00" + record.SessionID
			if seenRuns[identity] {
				return ModelRecommendation{}, fmt.Errorf("duplicate live run identity")
			}
			seenRuns[identity] = true
			run := record.Run
			if record.AgentError != "" {
				run.ExecutionError = record.AgentError
			}
			report, err := Score([]Scenario{scenario}, []Run{run})
			if err != nil {
				return ModelRecommendation{}, err
			}
			counts[record.Scenario]++
			candidate.Samples++
			successes += report.Successful
			candidate.MeanCost += record.Cost
			candidate.MeanDurationMS += float64(record.DurationMS)
		}
		for id := range selected {
			if counts[id] < minimum {
				return ModelRecommendation{}, fmt.Errorf("insufficient comparable live evidence for %s/%s: need %d samples", model, id, minimum)
			}
		}
		candidate.SuccessRate = float64(successes) / float64(candidate.Samples)
		candidate.MeanCost /= float64(candidate.Samples)
		candidate.MeanDurationMS /= float64(candidate.Samples)
		if candidate.SuccessRate < threshold || policy.MaxMeanCost > 0 && candidate.MeanCost > policy.MaxMeanCost || policy.MaxMeanDurationMS > 0 && candidate.MeanDurationMS > float64(policy.MaxMeanDurationMS) {
			continue
		}
		ranked = append(ranked, candidate)
	}
	if len(ranked) == 0 {
		return ModelRecommendation{}, fmt.Errorf("no candidate satisfies the quality, cost and duration thresholds")
	}
	// Require equal sampling per scenario so extra easy cases cannot skew ranking.
	for _, id := range policy.Scenarios {
		counts := []int{}
		for _, model := range policy.Candidates {
			n := 0
			for _, r := range records {
				if r.Scenario == id && r.Model == model && r.Role == policy.Role && r.PromptVersion == policy.PromptVersion && r.RecordedAt > 0 && r.RecordedAt <= now.Unix() && now.Sub(time.Unix(r.RecordedAt, 0)) <= time.Duration(age)*time.Hour && r.MetricsSource == "cumulative_runtime_ledger" && r.SessionID != "" && r.Workspace != "" {
					n++
				}
			}
			counts = append(counts, n)
		}
		if !slices.Equal(counts, slices.Repeat([]int{counts[0]}, len(counts))) {
			return ModelRecommendation{}, fmt.Errorf("candidate sample counts differ for scenario %s", id)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.SuccessRate != b.SuccessRate {
			return a.SuccessRate > b.SuccessRate
		}
		if a.MeanCost != b.MeanCost {
			return a.MeanCost < b.MeanCost
		}
		if a.MeanDurationMS != b.MeanDurationMS {
			return a.MeanDurationMS < b.MeanDurationMS
		}
		return a.Model < b.Model
	})
	return ranked[0], nil
}
