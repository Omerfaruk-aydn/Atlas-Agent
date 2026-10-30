package attempt

import (
	"fmt"
	"sort"
)

// Scoring weights. Verification dominates because it is the only signal
// that executes the code; lint is cheaper evidence; size is a tiebreaker
// that prefers the smaller change when everything else is equal.
const (
	weightVerify = 75.0
	weightLint   = 15.0
	weightSize   = 10.0

	// judgeShare is how much of the final score the judge decides when a
	// judge ran. The deterministic score keeps the larger share.
	judgeShare = 0.4
)

func passedFraction(checks []CheckResult) float64 {
	if len(checks) == 0 {
		return 0
	}
	n := 0
	for _, c := range checks {
		if c.Passed {
			n++
		}
	}
	return float64(n) / float64(len(checks))
}

// scoreDeterministic fills Deterministic and VerifyPassed for every
// eligible attempt. wantVerify and wantLint say whether those check
// families were configured, so a missing family is left out of the
// weighting instead of counting as a pass.
func scoreDeterministic(attempts []Attempt, wantVerify, wantLint bool) {
	minLines := 0
	for i := range attempts {
		a := &attempts[i]
		if !a.Eligible {
			continue
		}
		if lines := a.Additions + a.Deletions; lines > 0 && (minLines == 0 || lines < minLines) {
			minLines = lines
		}
	}
	for i := range attempts {
		a := &attempts[i]
		a.VerifyPassed = !wantVerify || (len(a.Checks) > 0 && passedFraction(a.Checks) == 1)
		if !a.Eligible {
			continue
		}
		var total, weight float64
		if wantVerify {
			total += weightVerify * passedFraction(a.Checks)
			weight += weightVerify
		}
		if wantLint {
			total += weightLint * passedFraction(a.Lint)
			weight += weightLint
		}
		size := 1.0
		if lines := a.Additions + a.Deletions; lines > 0 && minLines > 0 {
			size = float64(minLines) / float64(lines)
		}
		total += weightSize * size
		weight += weightSize
		a.Deterministic = round1(100 * total / weight)
	}
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// combine sets Final for each eligible attempt from the deterministic
// score and, when present, the judge's verdict.
func combine(attempts []Attempt) {
	for i := range attempts {
		a := &attempts[i]
		if !a.Eligible {
			a.Final = 0
			continue
		}
		a.Final = a.Deterministic
		if a.Judge != nil {
			a.Final = round1((1-judgeShare)*a.Deterministic + judgeShare*a.Judge.Score*10)
		}
	}
}

// rank orders the eligible attempts and returns the winner's index (0 when
// there is none). An attempt that fails a verify command never outranks one
// that passes them all, whatever the judge thought: taste does not override
// a failing test.
func rank(attempts []Attempt) int {
	order := make([]int, 0, len(attempts))
	for i := range attempts {
		attempts[i].Rank = 0
		if attempts[i].Eligible {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := attempts[order[x]], attempts[order[y]]
		if a.VerifyPassed != b.VerifyPassed {
			return a.VerifyPassed
		}
		if a.Final != b.Final {
			return a.Final > b.Final
		}
		if la, lb := a.Additions+a.Deletions, b.Additions+b.Deletions; la != lb {
			return la < lb
		}
		return a.Index < b.Index
	})
	for r, i := range order {
		attempts[i].Rank = r + 1
	}
	if len(order) == 0 {
		return 0
	}
	return attempts[order[0]].Index
}

// assess states how far the winner can be trusted, with the reasons.
func assess(r *Report) (string, []string) {
	var winner *Attempt
	eligible := 0
	for i := range r.Attempts {
		if r.Attempts[i].Eligible {
			eligible++
		}
		if r.Attempts[i].Index == r.Winner {
			winner = &r.Attempts[i]
		}
	}
	if winner == nil {
		return ConfidenceNone, []string{"no attempt produced an applicable change"}
	}
	var reasons []string
	level := ConfidenceHigh
	lower := func(to string, why string) {
		reasons = append(reasons, why)
		if to == ConfidenceLow || level == ConfidenceHigh {
			level = to
		}
	}
	if len(r.Verify) == 0 {
		lower(ConfidenceLow, "no verify commands were available, so the ranking rests on the judge and diff size only")
	} else if !winner.VerifyPassed {
		lower(ConfidenceLow, "the winner fails at least one verify command")
	}
	if r.Judge.Ran && r.Judge.Winner != "" && r.Judge.Winner != winner.Label {
		lower(ConfidenceLow, fmt.Sprintf("the judge preferred attempt %s over the winner", r.Judge.Winner))
	}
	if eligible == 1 {
		lower(ConfidenceMedium, "only one attempt produced an applicable change, so nothing was compared")
	}
	if !r.Judge.Ran && eligible > 1 {
		lower(ConfidenceMedium, "no judge verdict is available")
	}
	for i := range r.Attempts {
		other := &r.Attempts[i]
		if other.Eligible && other.Index != winner.Index && other.Rank == 2 && winner.Final-other.Final < 5 && other.PatchHash != winner.PatchHash {
			lower(ConfidenceMedium, fmt.Sprintf("attempt %s scored within 5 points of the winner", other.Label))
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "the winner passes every check and leads the other attempts")
	}
	return level, reasons
}
