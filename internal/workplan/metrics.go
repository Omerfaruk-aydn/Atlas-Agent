package workplan

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Agents cannot feel time passing: a feature that has eaten ten turns looks
// the same from inside as one that took a single turn. Atlas attributes each
// finished turn's time and tokens to the feature being worked on, the first
// open one, and shows the running cost in the plan summary with a warning
// when one feature costs far more than the finished ones did. That is the
// signal to step back and change approach rather than keep pushing.

// MetricsFile holds per-feature cost, written by Atlas.
const MetricsFile = "work-metrics.json"

// FeatureCost is what one feature has cost so far.
type FeatureCost struct {
	Turns  int   `json:"turns"`
	Millis int64 `json:"millis"`
	Tokens int64 `json:"tokens"`
}

// Costs maps feature ids to their cost.
type Costs map[string]FeatureCost

// LoadCosts reads the recorded costs; a missing file is an empty record.
func LoadCosts(root string) (Costs, error) {
	data, err := os.ReadFile(filepath.Join(root, Dir, MetricsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Costs{}, nil
	}
	if err != nil {
		return nil, err
	}
	costs := Costs{}
	if err := json.Unmarshal(data, &costs); err != nil {
		return Costs{}, nil //nolint:nilerr // A corrupt record restarts the count.
	}
	return costs, nil
}

// AddTurn charges one turn to the first open feature of the plan at root.
// It does nothing without a plan or an open feature.
func AddTurn(root string, elapsed time.Duration, tokens int64) error {
	plan, _, err := Load(root)
	if err != nil || plan == nil {
		return err
	}
	open := plan.Open()
	if len(open) == 0 {
		return nil
	}
	costs, err := LoadCosts(root)
	if err != nil {
		return err
	}
	cost := costs[open[0].ID]
	cost.Turns++
	cost.Millis += elapsed.Milliseconds()
	cost.Tokens += max(tokens, 0)
	costs[open[0].ID] = cost
	data, err := json.MarshalIndent(costs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, Dir, MetricsFile), data, 0o644)
}

// costWarningFactor is how many times the median finished feature the
// current one may cost before the summary warns.
const (
	costWarningFactor = 3
	costWarningTurns  = 12
)

// costWarning returns a warning when the current feature costs far more
// than the finished ones, or has taken many turns when there is nothing to
// compare with yet.
func costWarning(plan *Plan, costs Costs) string {
	open := plan.Open()
	if len(open) == 0 {
		return ""
	}
	current, ok := costs[open[0].ID]
	if !ok || current.Turns == 0 {
		return ""
	}
	var finished []int
	for _, id := range plan.DoneIDs() {
		if c, ok := costs[id]; ok && c.Turns > 0 {
			finished = append(finished, c.Turns)
		}
	}
	if len(finished) >= 2 {
		slices.Sort(finished)
		median := finished[len(finished)/2]
		if current.Turns >= costWarningFactor*max(median, 1) && current.Turns >= 3 {
			return "far longer than finished features took (median " + plural(median, "turn") + ")"
		}
		return ""
	}
	if current.Turns >= costWarningTurns {
		return "many turns without finishing"
	}
	return ""
}
