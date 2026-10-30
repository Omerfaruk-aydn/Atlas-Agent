package evaluation

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// The statistics here are deliberately conservative. A bench suite is a
// small sample of tasks, and attempts of one task are not independent, so
// the task is the unit of comparison: each task contributes one paired
// difference between the candidate and the baseline, and a verdict needs
// both a sign test and a bootstrap interval over tasks to agree.

const (
	// improveAlpha is the two-sided significance level of the sign test.
	improveAlpha = 0.05
	// improveBootstrapRounds is the number of task resamples.
	improveBootstrapRounds = 4000
	// improveCeiling is the baseline pass rate above which a suite can no
	// longer show an improvement.
	improveCeiling = 0.95
	// improveMinRepeats is the fewest repeats per task that make flakiness
	// visible at all.
	improveMinRepeats = 3
)

// Verdict is the outcome of comparing a candidate with the baseline.
type Verdict string

// The possible verdicts of an A/B comparison.
const (
	VerdictImproved     Verdict = "improved"
	VerdictRegressed    Verdict = "regressed"
	VerdictInconclusive Verdict = "inconclusive"
	VerdictNoData       Verdict = "no_data"
)

// VariantStats describes one variant's valid runs.
type VariantStats struct {
	Variant    string  `json:"variant"`
	Runs       int     `json:"runs"`
	Valid      int     `json:"valid_runs"`
	Excluded   int     `json:"excluded_runs"`
	Passed     int     `json:"passed"`
	PassRate   float64 `json:"pass_rate"`
	PassLow    float64 `json:"pass_rate_ci_low"`
	PassHigh   float64 `json:"pass_rate_ci_high"`
	MeanScore  float64 `json:"mean_score"`
	MeanCost   float64 `json:"mean_cost_usd"`
	MeanTokens float64 `json:"mean_tokens"`
	// RepeatStdDev is the standard deviation of the pass rate across
	// repeats (attempt rounds); it is the run-to-run noise of this variant.
	RepeatStdDev float64 `json:"repeat_pass_rate_stddev"`
	// FlakyTasks counts tasks that both passed and failed across repeats.
	FlakyTasks int `json:"flaky_tasks"`
}

// TaskDelta is the paired difference on one task.
type TaskDelta struct {
	Task          string  `json:"task"`
	BaselineRate  float64 `json:"baseline_pass_rate"`
	CandidateRate float64 `json:"candidate_pass_rate"`
	ScoreDelta    float64 `json:"score_delta"`
}

// ABComparison is the honest result of one A/B run.
type ABComparison struct {
	Baseline  VariantStats `json:"baseline"`
	Candidate VariantStats `json:"candidate"`
	// PairedTasks counts tasks both variants have valid runs for.
	PairedTasks int         `json:"paired_tasks"`
	Wins        int         `json:"wins"`
	Losses      int         `json:"losses"`
	Ties        int         `json:"ties"`
	SignTestP   float64     `json:"sign_test_p"`
	ScoreDelta  float64     `json:"score_delta"`
	DeltaLow    float64     `json:"score_delta_ci_low"`
	DeltaHigh   float64     `json:"score_delta_ci_high"`
	CostDelta   float64     `json:"cost_delta_pct"`
	TokensDelta float64     `json:"tokens_delta_pct"`
	Deltas      []TaskDelta `json:"task_deltas"`
	Verdict     Verdict     `json:"verdict"`
	Warnings    []string    `json:"warnings,omitempty"`
}

// CompareVariants compares candidate with baseline over their bench
// records. Runs that never reached the model or failed at the provider are
// excluded from both sides, as in SummarizeBench.
func CompareVariants(records []BenchRecord, baseline, candidate string) ABComparison {
	base := newVariantAcc(baseline)
	cand := newVariantAcc(candidate)
	for _, r := range records {
		switch r.Variant {
		case baseline:
			base.add(r)
		case candidate:
			cand.add(r)
		}
	}
	c := ABComparison{Baseline: base.stats(), Candidate: cand.stats()}

	tasks := make([]string, 0, len(base.byTask))
	for task := range base.byTask {
		if _, ok := cand.byTask[task]; ok {
			tasks = append(tasks, task)
		}
	}
	sort.Strings(tasks)
	diffs := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		b, k := base.byTask[task], cand.byTask[task]
		d := TaskDelta{Task: task, BaselineRate: b.rate(), CandidateRate: k.rate(), ScoreDelta: k.meanScore() - b.meanScore()}
		c.Deltas = append(c.Deltas, d)
		diffs = append(diffs, d.ScoreDelta)
		switch {
		case d.CandidateRate > d.BaselineRate, d.CandidateRate == d.BaselineRate && d.ScoreDelta > 0:
			c.Wins++
		case d.CandidateRate < d.BaselineRate, d.CandidateRate == d.BaselineRate && d.ScoreDelta < 0:
			c.Losses++
		default:
			c.Ties++
		}
	}
	c.PairedTasks = len(tasks)
	c.SignTestP = signTestP(c.Wins, c.Losses)
	if len(diffs) > 0 {
		c.ScoreDelta = mean(diffs)
		c.DeltaLow, c.DeltaHigh = bootstrapMeanCI(diffs)
	}
	c.CostDelta = percentChange(c.Baseline.MeanCost, c.Candidate.MeanCost)
	c.TokensDelta = percentChange(c.Baseline.MeanTokens, c.Candidate.MeanTokens)

	switch {
	case c.PairedTasks == 0:
		c.Verdict = VerdictNoData
	case c.SignTestP < improveAlpha && c.Wins > c.Losses && c.DeltaLow > 0:
		c.Verdict = VerdictImproved
	case c.SignTestP < improveAlpha && c.Losses > c.Wins && c.DeltaHigh < 0:
		c.Verdict = VerdictRegressed
	default:
		c.Verdict = VerdictInconclusive
	}
	c.Warnings = comparisonWarnings(c, base, cand)
	return c
}

type taskAcc struct {
	valid, passed int
	score         float64
}

func (t taskAcc) rate() float64 {
	if t.valid == 0 {
		return 0
	}
	return float64(t.passed) / float64(t.valid)
}

func (t taskAcc) meanScore() float64 {
	if t.valid == 0 {
		return 0
	}
	return t.score / float64(t.valid)
}

type variantAcc struct {
	name           string
	runs, excluded int
	valid, passed  int
	score, cost    float64
	tokens         float64
	byTask         map[string]taskAcc
	byAttempt      map[int]taskAcc
}

func newVariantAcc(name string) *variantAcc {
	return &variantAcc{name: name, byTask: map[string]taskAcc{}, byAttempt: map[int]taskAcc{}}
}

func (a *variantAcc) add(r BenchRecord) {
	a.runs++
	if r.ErrorKind == benchErrorUnreached || r.ErrorKind == ErrorProvider {
		a.excluded++
		return
	}
	score := r.Score
	// Records written before scores existed carry 0 for a pass.
	if r.Passed && score == 0 {
		score = 100
	}
	a.valid++
	a.score += score
	a.cost += r.Cost
	a.tokens += float64(r.Tokens)
	bump := func(t taskAcc) taskAcc {
		t.valid++
		t.score += score
		if r.Passed {
			t.passed++
		}
		return t
	}
	if r.Passed {
		a.passed++
	}
	a.byTask[r.Task] = bump(a.byTask[r.Task])
	a.byAttempt[r.Attempt] = bump(a.byAttempt[r.Attempt])
}

func (a *variantAcc) stats() VariantStats {
	s := VariantStats{Variant: a.name, Runs: a.runs, Valid: a.valid, Excluded: a.excluded, Passed: a.passed}
	if a.valid == 0 {
		return s
	}
	n := float64(a.valid)
	s.PassRate = float64(a.passed) / n
	s.PassLow, s.PassHigh = wilsonInterval(a.passed, a.valid)
	s.MeanScore, s.MeanCost, s.MeanTokens = a.score/n, a.cost/n, a.tokens/n
	rates := make([]float64, 0, len(a.byAttempt))
	for _, t := range a.byAttempt {
		rates = append(rates, t.rate())
	}
	s.RepeatStdDev = stdDev(rates)
	for _, t := range a.byTask {
		if t.passed > 0 && t.passed < t.valid {
			s.FlakyTasks++
		}
	}
	return s
}

func comparisonWarnings(c ABComparison, base, cand *variantAcc) []string {
	var w []string
	if c.PairedTasks == 0 {
		return append(w, "No task has valid runs under both variants, so nothing can be compared.")
	}
	if c.Baseline.Excluded > 0 || c.Candidate.Excluded > 0 {
		w = append(w, fmt.Sprintf("%d baseline and %d candidate runs were excluded (provider failures or never reached the model); a lopsided exclusion can bias the comparison.", c.Baseline.Excluded, c.Candidate.Excluded))
	}
	// The sign test over tasks cannot reach alpha with too few untied tasks.
	if minTasks := minTasksForSignificance(); c.PairedTasks < minTasks {
		w = append(w, fmt.Sprintf("Only %d paired tasks: a sign test needs at least %d untied tasks all in one direction to reach p<%.2f, so the verdict can only be inconclusive.", c.PairedTasks, minTasks, improveAlpha))
	}
	if r := maxAttempts(base, cand); r < improveMinRepeats {
		w = append(w, fmt.Sprintf("Each task ran at most %d time(s). With fewer than %d repeats run-to-run variance is invisible and a lucky run looks like a gain.", r, improveMinRepeats))
	}
	if c.Baseline.PassRate >= improveCeiling && c.Baseline.Valid > 0 {
		w = append(w, fmt.Sprintf("Ceiling: the baseline already passes %.0f%% of runs, so this suite cannot show an improvement. Mine harder tasks.", c.Baseline.PassRate*100))
	}
	if c.Baseline.Passed == 0 && c.Candidate.Passed == 0 {
		w = append(w, "Floor: neither variant passed a single run, so the tasks may be unsolvable or the checks broken rather than informative.")
	}
	if c.Baseline.FlakyTasks+c.Candidate.FlakyTasks > 0 {
		w = append(w, fmt.Sprintf("Flaky tasks (passed and failed across repeats): %d baseline, %d candidate. Their outcomes are partly luck.", c.Baseline.FlakyTasks, c.Candidate.FlakyTasks))
	}
	if c.Wins == 0 && c.Losses == 0 {
		w = append(w, "Every paired task tied; the variants behaved identically on this suite.")
	}
	if c.Verdict == VerdictImproved && c.CostDelta > 25 {
		w = append(w, fmt.Sprintf("The candidate costs %.0f%% more per run than the baseline; weigh the gain against it.", c.CostDelta))
	}
	return w
}

func maxAttempts(variants ...*variantAcc) int {
	most := 0
	for _, v := range variants {
		for _, t := range v.byTask {
			most = max(most, t.valid)
		}
	}
	return most
}

// minTasksForSignificance is the smallest number of untied tasks, all won
// by one side, for which the two-sided sign test reaches improveAlpha.
func minTasksForSignificance() int {
	for n := 1; n < 64; n++ {
		if signTestP(n, 0) < improveAlpha {
			return n
		}
	}
	return 64
}

// signTestP is the exact two-sided sign test p-value for wins against
// losses, ignoring ties.
func signTestP(wins, losses int) float64 {
	n := wins + losses
	if n == 0 {
		return 1
	}
	k := min(wins, losses)
	// P(X <= k) for X ~ Binomial(n, 1/2), doubled for two sides.
	var tail float64
	for i := 0; i <= k; i++ {
		tail += math.Exp(logChoose(n, i) - float64(n)*math.Ln2)
	}
	return min(1, 2*tail)
}

func logChoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// wilsonInterval is the 95% Wilson score interval of a proportion; unlike
// the normal approximation it stays sensible at 0% and 100%.
func wilsonInterval(passed, n int) (low, high float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.959964
	p := float64(passed) / float64(n)
	nn := float64(n)
	denom := 1 + z*z/nn
	center := (p + z*z/(2*nn)) / denom
	half := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / denom
	return max(0, center-half), min(1, center+half)
}

// bootstrapMeanCI is a percentile bootstrap 95% interval of the mean of
// values. The seed is fixed so a report is reproducible.
func bootstrapMeanCI(values []float64) (low, high float64) {
	if len(values) == 0 {
		return 0, 0
	}
	rng := rand.New(rand.NewPCG(1, 2))
	means := make([]float64, improveBootstrapRounds)
	for i := range means {
		var sum float64
		for range values {
			sum += values[rng.IntN(len(values))]
		}
		means[i] = sum / float64(len(values))
	}
	sort.Float64s(means)
	return means[int(0.025*float64(len(means)))], means[int(0.975*float64(len(means)))-1]
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stdDev is the sample standard deviation; it is 0 below two values.
func stdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m := mean(values)
	var sum float64
	for _, v := range values {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

func percentChange(from, to float64) float64 {
	if from == 0 {
		return 0
	}
	return (to - from) / from * 100
}
