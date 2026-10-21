package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func readAlternativeSummary(dir, id, command string) AlternativeSummary {
	summary := AlternativeSummary{ID: id, Approach: "Verified worktree approach", Checks: []string{command}}
	if data, err := os.ReadFile(filepath.Join(dir, ".atlas", "alternative-summary.json")); err == nil {
		var supplied AlternativeSummary
		if json.Unmarshal(data, &supplied) == nil {
			summary = supplied
			summary.ID = id
			summary.Checks = []string{command}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10_000_000_000)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--binary", "HEAD").CombinedOutput(); err == nil {
		summary.Diff = string(output)
	}
	return summary
}

// AlternativeSummary is the evidence required from every parallel attempt.
type AlternativeSummary struct {
	ID          string   `json:"id"`
	Approach    string   `json:"approach"`
	Files       []string `json:"files"`
	Checks      []string `json:"checks"`
	Regressions int      `json:"regressions"`
	Risks       []string `json:"risks"`
	Diff        string   `json:"diff"`
}

// EligibleAlternatives eliminates failed builds/contracts and orders the
// remaining candidates by observed regressions before model judging.
func EligibleAlternatives(summaries []AlternativeSummary) ([]AlternativeSummary, error) {
	var eligible []AlternativeSummary
	for _, summary := range summaries {
		if summary.ID == "" || strings.TrimSpace(summary.Approach) == "" {
			return nil, errors.New("every alternative needs an ID and approach")
		}
		if summary.Regressions < 0 {
			return nil, errors.New("regression count cannot be negative")
		}
		if summary.Regressions == 0 {
			eligible = append(eligible, summary)
		}
	}
	return eligible, nil
}

func tournamentPrompt(left, right AlternativeSummary) string {
	encode := func(summary AlternativeSummary) string {
		copy := summary
		if len(copy.Diff) > 24000 {
			copy.Diff = copy.Diff[:24000] + "\n[diff truncated]"
		}
		data, _ := json.Marshal(copy)
		return string(data)
	}
	return fmt.Sprintf("Compare these two verified implementation candidates. Choose the lower-risk complete solution, not the shorter answer. Return only the winning ID.\nA:\n%s\nB:\n%s", encode(left), encode(right))
}

func (a *sessionAgent) tournamentWinner(ctx context.Context, sessionID string, candidates []AlternativeSummary) string {
	eligible, err := EligibleAlternatives(candidates)
	if err != nil || len(eligible) == 0 {
		return ""
	}
	winner := eligible[0]
	for _, challenger := range eligible[1:] {
		model := a.smallModel.Get()
		if model.Model == nil {
			continue
		}
		reply, err := a.runReviewer(ctx, sessionID, model, "You judge implementation candidates by evidence. Return only one supplied ID.", tournamentPrompt(winner, challenger), nil, nil, 1)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(reply) {
		case challenger.ID:
			winner = challenger
		case winner.ID:
		default:
			// Invalid judgments fail closed to the deterministic incumbent.
		}
	}
	return winner.ID
}
