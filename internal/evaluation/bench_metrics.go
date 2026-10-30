package evaluation

import (
	"fmt"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

type BenchMetric struct {
	Variant    string  `json:"variant"`
	Tasks      int     `json:"tasks"`
	PassAt1    float64 `json:"pass_at_1"`
	PassAtK    float64 `json:"pass_at_k"`
	FailOpen   int     `json:"fail_open"`
	Unmeasured int     `json:"gate_metrics_unmeasured_runs"`
	Tokens     int64   `json:"tokens"`
	Cost       float64 `json:"cost_usd"`
	DurationMS int64   `json:"duration_ms"`
}

// BenchMetrics compares observed first and any valid attempt for each task.
// Provider failures and unreached attempts follow the existing exclusions.
func BenchMetrics(records []BenchRecord) []BenchMetric {
	type task struct {
		first                  int
		firstPassed, anyPassed bool
	}
	by := map[string]map[string]*task{}
	totals := map[string]*BenchMetric{}
	for _, r := range records {
		if totals[r.Variant] == nil {
			totals[r.Variant] = &BenchMetric{Variant: r.Variant}
			by[r.Variant] = map[string]*task{}
		}
		m := totals[r.Variant]
		m.Tokens += r.Tokens
		m.Cost += r.Cost
		m.DurationMS += r.DurationMS
		m.FailOpen += r.FailOpen
		if !r.GateMetricsMeasured {
			m.Unmeasured++
		}
		if r.ErrorKind == ErrorProvider || r.ErrorKind == benchErrorUnreached {
			continue
		}
		t := by[r.Variant][r.Task]
		if t == nil {
			t = &task{first: r.Attempt, firstPassed: r.Passed}
			by[r.Variant][r.Task] = t
		} else if r.Attempt < t.first {
			t.first = r.Attempt
			t.firstPassed = r.Passed
		}
		t.anyPassed = t.anyPassed || r.Passed
	}
	out := []BenchMetric{}
	for variant, m := range totals {
		m.Tasks = len(by[variant])
		for _, t := range by[variant] {
			if t.firstPassed {
				m.PassAt1++
			}
			if t.anyPassed {
				m.PassAtK++
			}
		}
		if m.Tasks > 0 {
			m.PassAt1 /= float64(m.Tasks)
			m.PassAtK /= float64(m.Tasks)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Variant < out[j].Variant })
	return out
}

// BenchRegressions compares only matched variant/task observations.
func BenchRegressions(current, previous []BenchRecord) []string {
	before := SummarizeBench(previous)
	after := SummarizeBench(current)
	warnings := []string{}
	for _, a := range after {
		for _, b := range before {
			if a.Variant != b.Variant {
				continue
			}
			for name, t := range a.PerTask {
				old, ok := b.PerTask[name]
				if ok && old.Valid > 0 && t.Valid > 0 && float64(t.Passed)/float64(t.Valid) < float64(old.Passed)/float64(old.Valid) {
					warnings = append(warnings, fmt.Sprintf("Regression: %s / %s pass rate decreased", a.Variant, name))
				}
			}
		}
	}
	sort.Strings(warnings)
	return warnings
}

func benchTraceFailures(root, session string) (int, bool) {
	if session == "" {
		return 0, false
	}
	events, err := harnessmetrics.LoadTrace(root, session, 0)
	if err != nil || len(events) == 0 {
		return 0, false
	}
	pending := map[string]bool{}
	scheduled := map[string]bool{}
	for _, e := range events {
		key := fmt.Sprintf("%s/%d/%s", e.TraceID, e.Turn, e.Name)
		if e.Kind == "model_start" {
			pending[e.CallID] = true
		}
		if e.Kind == "model" {
			delete(pending, e.CallID)
		}
		if e.Kind == "helper_scheduled" {
			scheduled[key] = true
		}
		if e.Kind == "gate" {
			delete(scheduled, key)
		}
	}
	count := 0
	for _, g := range harnessmetrics.SummarizeGates(events) {
		count += g.FailOpen
	}
	return count, len(pending) == 0 && len(scheduled) == 0
}
