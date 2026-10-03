// Package experiments implements bounded, observed engineering experiments.
package experiments

import (
	"context"
	"fmt"
	"strings"
)

type Observation struct {
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
}

type Oracle func(context.Context, string) (Observation, error)

type Reduction struct {
	Input           string      `json:"input"`
	Runs            int         `json:"runs"`
	BudgetExhausted bool        `json:"budget_exhausted"`
	Observation     Observation `json:"observation"`
}

// Minimize removes line chunks while preserving an exit and output signature.
func Minimize(ctx context.Context, input string, exit int, marker string, budget int, run Oracle) (Reduction, error) {
	result := Reduction{Input: input}
	if exit == 0 || strings.TrimSpace(marker) == "" || len(input) > 32768 || budget < 2 || budget > 64 || run == nil {
		return result, fmt.Errorf("require a nonzero exit, failure marker, input <=32KiB and run budget 2-64")
	}
	observe := func(candidate string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		observation, err := run(ctx, candidate)
		result.Runs++
		if err != nil {
			return false, err
		}
		matches := observation.ExitCode == exit && strings.Contains(observation.Output, marker)
		if matches {
			result.Input, result.Observation = candidate, observation
		}
		return matches, nil
	}
	matched, err := observe(input)
	if err != nil {
		return result, err
	}
	if !matched {
		return result, fmt.Errorf("baseline does not reproduce the requested failure")
	}
	parts := strings.SplitAfter(input, "\n")
	for chunk := max(1, len(parts)/2); chunk >= 1; chunk /= 2 {
		for i := 0; i < len(parts); {
			if result.Runs >= budget {
				result.BudgetExhausted = true
				return result, nil
			}
			end := min(i+chunk, len(parts))
			candidate := strings.Join(parts[:i], "") + strings.Join(parts[end:], "")
			matched, err := observe(candidate)
			if err != nil {
				return result, err
			}
			if matched {
				parts = append(parts[:i:i], parts[end:]...)
			} else {
				i = end
			}
		}
	}
	return result, nil
}
