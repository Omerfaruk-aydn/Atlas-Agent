package evaluation

import (
	"fmt"
	"slices"
)

type PromptComparison struct {
	MeanUnnecessaryQuestionsDelta *float64 `json:"mean_unnecessary_questions_delta,omitempty"`
	Model                         string   `json:"model"`
	Role                          string   `json:"role,omitempty"`
	BeforeVersion                 string   `json:"before_version"`
	AfterVersion                  string   `json:"after_version"`
	Samples                       int      `json:"samples_per_version"`
	Before                        Report   `json:"before"`
	After                         Report   `json:"after"`
	AcceptanceDelta               float64  `json:"acceptance_delta"`
	MeanTokensDelta               float64  `json:"mean_tokens_delta"`
	MeanCostDelta                 float64  `json:"mean_cost_usd_delta"`
	MeanDurationDelta             float64  `json:"mean_duration_ms_delta"`
	Limit                         string   `json:"interpretation_limit"`
}

// ComparePromptVersions rejects unlike fixtures and unequal sample counts.
// It reports observed deltas rather than asserting statistical significance.
func ComparePromptVersions(scenarios []Scenario, before, after []LiveRecord, minimum int) ([]PromptComparison, error) {
	if minimum < 1 || minimum > 20 || len(before) == 0 || len(after) == 0 || len(before) > 256 || len(after) > 256 {
		return nil, fmt.Errorf("comparison requires two bounded sets and minimum samples 1-20")
	}
	type key struct{ model, role, scenario, baseline, fixture string }
	type group struct{ model, role string }
	sets := [2]map[key][]Run{{}, {}}
	versions := [2]string{}
	seenSessions := map[string]bool{}
	for i, records := range [][]LiveRecord{before, after} {
		for _, record := range records {
			if record.PromptVersion == "" || record.Model == "" || record.Baseline == "" || record.CaseHash == "" || record.SessionID == "" || record.MetricsSource != "cumulative_runtime_ledger" {
				return nil, fmt.Errorf("comparison requires case hashes, baseline, session and runtime metrics")
			}
			if seenSessions[record.SessionID] {
				return nil, fmt.Errorf("comparison samples must use distinct sessions")
			}
			seenSessions[record.SessionID] = true
			if versions[i] == "" {
				versions[i] = record.PromptVersion
			}
			if record.PromptVersion != versions[i] {
				return nil, fmt.Errorf("each input must contain one prompt version")
			}
			k := key{record.Model, record.Role, record.Scenario, record.Baseline, record.CaseHash}
			observed := record.ClaimObserved
			record.Run.ClaimObserved = &observed
			sets[i][k] = append(sets[i][k], record.Run)
		}
	}
	if versions[0] == versions[1] || len(sets[0]) != len(sets[1]) {
		return nil, fmt.Errorf("distinct prompt versions with matched cases are required")
	}
	grouped := map[group][2][]Run{}
	keys := make([]key, 0, len(sets[0]))
	for k := range sets[0] {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int {
		return slices.Compare([]string{a.model, a.role, a.scenario, a.baseline, a.fixture}, []string{b.model, b.role, b.scenario, b.baseline, b.fixture})
	})
	for _, k := range keys {
		old := sets[0][k]
		fresh, ok := sets[1][k]
		if !ok || len(old) != len(fresh) || len(old) < minimum {
			return nil, fmt.Errorf("unmatched fixture or insufficient/equal samples for %s/%s/%s", k.model, k.role, k.scenario)
		}
		g := group{k.model, k.role}
		pair := grouped[g]
		pair[0] = append(pair[0], old...)
		pair[1] = append(pair[1], fresh...)
		grouped[g] = pair
	}
	var out []PromptComparison
	for g, pair := range grouped {
		old, err := scoreSamples(scenarios, pair[0])
		if err != nil {
			return nil, err
		}
		fresh, err := scoreSamples(scenarios, pair[1])
		if err != nil {
			return nil, err
		}
		n := float64(len(pair[0]))
		var questionDelta *float64
		if old.QuestionReviewedSamples == len(pair[0]) && fresh.QuestionReviewedSamples == len(pair[1]) {
			delta := float64(fresh.UnnecessaryQuestions-old.UnnecessaryQuestions) / n
			questionDelta = &delta
		}
		out = append(out, PromptComparison{MeanUnnecessaryQuestionsDelta: questionDelta, Model: g.model, Role: g.role, BeforeVersion: versions[0], AfterVersion: versions[1], Samples: len(pair[0]), Before: old, After: fresh, AcceptanceDelta: fresh.AcceptanceRate - old.AcceptanceRate, MeanTokensDelta: float64(fresh.Tokens-old.Tokens) / n, MeanCostDelta: (fresh.Cost - old.Cost) / n, MeanDurationDelta: float64(fresh.DurationMS-old.DurationMS) / n, Limit: "Matched recorded cases and runtime metrics; submitted evidence is not authenticated. Deltas do not establish statistical significance or quality on unseen tasks."})
	}
	slices.SortFunc(out, func(a, b PromptComparison) int {
		if a.Model < b.Model || a.Model == b.Model && a.Role < b.Role {
			return -1
		}
		if a.Model == b.Model && a.Role == b.Role {
			return 0
		}
		return 1
	})
	return out, nil
}
