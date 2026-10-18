package workplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// StateFile is Atlas's own bookkeeping about the plan, kept apart from the
// agent's files so the agent cannot accidentally reset it.
const StateFile = "work-state.json"

// State records what Atlas has already done for the plan.
type State struct {
	// ReviewedPlan is the hash of the plan version last reviewed.
	ReviewedPlan string `json:"reviewed_plan,omitempty"`
	// PlanReviews counts the reviews of the plan for ReviewedGoal, and
	// ReviewFindings holds the last one's findings, so a revision is checked
	// against them. A plan with another goal is new work and starts over.
	PlanReviews    int    `json:"plan_reviews,omitempty"`
	ReviewedGoal   string `json:"reviewed_goal,omitempty"`
	ReviewFindings string `json:"review_findings,omitempty"`
	// Settled are the features whose completion was already followed up
	// (probe, breaker pass), so each is followed up once.
	Settled []string `json:"settled,omitempty"`
	// Flaky are commands seen both passing and failing with no code change
	// in between.
	Flaky []string `json:"flaky,omitempty"`
}

// maxFlaky bounds the flaky commands remembered.
const maxFlaky = 20

// AddFlaky records command as flaky and reports whether it is new.
func (s *State) AddFlaky(command string) bool {
	command = normalize(command)
	if command == "" || slices.Contains(s.Flaky, command) {
		return false
	}
	s.Flaky = append(s.Flaky, command)
	s.Flaky = s.Flaky[max(0, len(s.Flaky)-maxFlaky):]
	return true
}

// IsFlaky reports whether command was recorded as flaky.
func (s State) IsFlaky(command string) bool {
	return slices.Contains(s.Flaky, normalize(command))
}

// LoadState reads the state; a missing or corrupt file is an empty state.
func LoadState(root string) State {
	data, err := os.ReadFile(filepath.Join(root, Dir, StateFile))
	if err != nil {
		return State{}
	}
	var s State
	if json.Unmarshal(data, &s) != nil {
		return State{}
	}
	return s
}

// SaveState writes the state.
func SaveState(root string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.WriteFile(filepath.Join(root, Dir, StateFile), data, 0o644)
}

// Unsettled returns the done features not yet followed up, in plan order.
func (s State) Unsettled(plan *Plan) []Feature {
	var out []Feature
	for _, feature := range plan.Features {
		if feature.Done && !slices.Contains(s.Settled, feature.ID) {
			out = append(out, feature)
		}
	}
	return out
}

// Hash is a short stable digest of a plan's review key.
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
