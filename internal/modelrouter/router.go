// Package modelrouter decides, step by step, whether an agent turn should run
// on a cheap fast model or on the strong model. The decision is a
// deterministic heuristic over the prompt wording (English and Turkish), the
// mix of tools used so far, error streaks, failing test output and the size
// of the context. It has no model, provider or storage dependencies so it can
// be tested exhaustively.
//
// The router is deliberately sticky. Provider prompt caches are per model, so
// every switch re-reads the whole context uncached; the router therefore
// moves to the strong model on a clear signal and returns to the fast model
// only after a streak of light work, a minimum dwell time and an estimate
// that the cheaper steps pay for the cache it gives up.
package modelrouter

import (
	"regexp"
	"strings"
	"sync"
)

// Tier is the class of model a step should run on.
type Tier string

// The two tiers.
const (
	TierFast   Tier = "fast"
	TierStrong Tier = "strong"
)

// Defaults for Config fields left at zero.
const (
	DefaultMinDwell        = 3
	DefaultDownshiftStreak = 2
	DefaultErrorStreak     = 2
	DefaultContextFraction = 0.7
	// remainingSteps is how many further steps a downshift is assumed to
	// serve when weighing it against the cache it gives up.
	remainingSteps = 4
)

// Config tunes the heuristic. Zero values select the defaults.
type Config struct {
	// MinDwell is the minimum number of steps between two switches.
	MinDwell int
	// DownshiftStreak is how many consecutive light steps must be seen
	// before moving from the strong to the fast tier.
	DownshiftStreak int
	// ErrorStreak is how many consecutive steps with failing tool calls
	// move work to the strong tier.
	ErrorStreak int
	// ContextFraction is the share of the fast model's context window above
	// which the fast model is no longer used.
	ContextFraction float64
}

func (c Config) minDwell() int {
	if c.MinDwell <= 0 {
		return DefaultMinDwell
	}
	return c.MinDwell
}

func (c Config) downshiftStreak() int {
	if c.DownshiftStreak <= 0 {
		return DefaultDownshiftStreak
	}
	return c.DownshiftStreak
}

func (c Config) errorStreak() int {
	if c.ErrorStreak <= 0 {
		return DefaultErrorStreak
	}
	return c.ErrorStreak
}

func (c Config) contextFraction() float64 {
	if c.ContextFraction <= 0 || c.ContextFraction > 1 {
		return DefaultContextFraction
	}
	return c.ContextFraction
}

// Prices are per million tokens. All zero means unknown.
type Prices struct {
	In         float64
	Out        float64
	CacheRead  float64
	CacheWrite float64
}

// Known reports whether any price is set.
func (p Prices) Known() bool {
	return p.In > 0 || p.Out > 0 || p.CacheRead > 0 || p.CacheWrite > 0
}

// ToolCall is one finished tool call of an earlier step.
type ToolCall struct {
	Name   string
	Failed bool
	// Output is the (possibly clipped) text the tool returned.
	Output string
}

// Step is the tool activity of one earlier step of the turn.
type Step struct {
	Calls []ToolCall
}

// Input is everything one decision looks at.
type Input struct {
	// StepNumber is zero for the first step of a turn.
	StepNumber int
	// Prompt is the user's prompt for the turn; it is read on step zero.
	Prompt string
	// Steps are the finished steps of this turn, oldest first.
	Steps []Step
	// ContextTokens is the size of the latest request, 0 when unknown.
	ContextTokens int64
	// FastWindow is the fast model's context window, 0 when unknown.
	FastWindow int64
	// NeedStrong forces the strong tier (for example when the turn carries
	// images the fast model cannot read).
	NeedStrong bool
	// Strong and Fast are the models' prices, used to weigh a downshift
	// against the cache it gives up.
	Strong, Fast Prices
	// CrossProvider reports that the fast and strong models belong to
	// different providers.
	CrossProvider bool
}

// Decision is the outcome for one step.
type Decision struct {
	Tier     Tier
	Reason   string
	Switched bool
}

// wordRe compiles alternatives delimited by non-letters. Go's \b is ASCII
// only, which would split Turkish words at their accented letters, and \pL
// lets an alternative carry any suffix.
func wordRe(alts string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^\pL])(?:` + alts + `)(?:[^\pL]|$)`)
}

var (
	strongPrompt = wordRe(`architect\pL*|design\pL*|refactor\pL*|debug\pL*|root[- ]cause|race condition|deadlock|memory leak|security|vulnerab\pL*|migrat\pL*|review\pL*|audit\pL*|investigat\pL*|diagnos\pL*|why (is|does|did|are|do)|failing tests?|test failures?|flaky|performance|optimi[sz]\pL*|trade-?offs?|complex|concurren\pL*|` +
		`mimari\pL*|tasar[ıi]m\pL*|hata ay[ıi]kla\pL*|kök neden\pL*|yar[ıi][şs] durumu|g[üu]venlik\pL*|zafiyet\pL*|g[öo]zden ge[çc]ir\pL*|inceleme\pL*|denetim\pL*|ba[şs]ar[ıi]s[ıi]z test\pL*|test hata\pL*|performans\pL*|optimize\pL*|neden|karma[şs][ıi]k`) //nolint:misspell // Turkish keyword.
	cheapPrompt = wordRe(`read|show|list|find|search|grep|locate|summari[sz]\pL*|explain|what is|where is|print|display|rename|typo|format|lint|comment|docstring|add a line|bump|` +
		`oku\pL*|g[öo]ster\pL*|listele\pL*|bul|ara|ara[şs]t[ıi]r\pL*|[öo]zetle\pL*|a[çc][ıi]kla\pL*|nerede|yeniden adland[ıi]r\pL*|yaz[ıi]m hatas[ıi]|bi[çc]imlendir\pL*|yorum ekle\pL*`)
	failureOutput = regexp.MustCompile(`(?m)(^|\s)(--- FAIL|FAIL\b|FAILED\b|panic:|Traceback \(most recent call last\)|AssertionError|npm ERR!|error\[E\d+\]|\d+ failed|build failed|compilation failed)`)
)

// readOnlyTools never change the workspace; a run of them is exploration.
var readOnlyTools = map[string]bool{
	"view": true, "grep": true, "glob": true, "ls": true, "fetch": true,
	"web_fetch": true, "web_search": true, "agentic_fetch": true, "sourcegraph": true,
	"lsp_diagnostics": true, "lsp_references": true, "lsp_definition": true, "lsp_hover": true,
	"read": true, "list": true, "find": true, "tool_search": true, "todos": true,
	"job_output": true,
}

// ClassifyPrompt scores a prompt. It returns TierStrong, TierFast or "" when
// the wording is not a clear signal either way. Word boundaries are ASCII
// only in Go's regexp, so Turkish suffixes are matched with explicit classes.
func ClassifyPrompt(prompt string) Tier {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return ""
	}
	strong := len(strongPrompt.FindAllString(p, -1))
	cheap := len(cheapPrompt.FindAllString(p, -1))
	switch {
	case strong > 0 && strong >= cheap:
		return TierStrong
	case cheap > 0 && strong == 0 && len([]rune(p)) <= 600:
		return TierFast
	default:
		return ""
	}
}

// errorStreak counts trailing steps in which a tool call failed.
func errorStreak(steps []Step) int {
	n := 0
	for i := len(steps) - 1; i >= 0; i-- {
		failed := false
		for _, c := range steps[i].Calls {
			if c.Failed {
				failed = true
				break
			}
		}
		if !failed {
			break
		}
		n++
	}
	return n
}

// testFailure reports whether the most recent step ran a command whose
// output looks like a failing test or build.
func testFailure(steps []Step) bool {
	if len(steps) == 0 {
		return false
	}
	for _, c := range steps[len(steps)-1].Calls {
		if c.Name == "bash" && failureOutput.MatchString(c.Output) {
			return true
		}
	}
	return false
}

func lightStep(s Step) bool {
	if len(s.Calls) == 0 {
		return false
	}
	for _, c := range s.Calls {
		if c.Failed || !readOnlyTools[c.Name] {
			return false
		}
	}
	return true
}

// lightStreak counts trailing steps that only used read-only tools.
func lightStreak(steps []Step) int {
	n := 0
	for i := len(steps) - 1; i >= 0 && lightStep(steps[i]); i-- {
		n++
	}
	return n
}

// downshiftPaysOff weighs the one-off cost of re-reading the context
// uncached on the fast model against the per-step saving of its cheaper
// cache reads. Unknown prices never block a downshift.
func downshiftPaysOff(in Input) bool {
	if !in.Strong.Known() || !in.Fast.Known() || in.ContextTokens <= 0 {
		return true
	}
	read := func(p Prices) float64 {
		if p.CacheRead > 0 {
			return p.CacheRead
		}
		return p.In
	}
	ctx := float64(in.ContextTokens) / 1e6
	perStep := ctx * (read(in.Strong) - read(in.Fast))
	prime := ctx * (max(in.Fast.CacheWrite, in.Fast.In) - read(in.Fast))
	return perStep*remainingSteps > prime
}

// Router holds the per-session state that makes decisions sticky. It is safe
// for concurrent use.
type Router struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*state
}

type state struct {
	tier         Tier
	sinceSwitch  int
	turnStrong   bool // The turn's prompt asked for strong work.
	turnPrompted bool // The turn's prompt was a clear signal either way.
}

// New returns a router with cfg.
func New(cfg Config) *Router {
	return &Router{cfg: cfg, sessions: map[string]*state{}}
}

// Decide returns the tier for one step of a session.
func (r *Router) Decide(sessionID string, in Input) Decision {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.sessions[sessionID]
	if st == nil {
		// The user chose the strong model; routing starts from it.
		st = &state{tier: TierStrong, sinceSwitch: r.cfg.minDwell()}
		r.sessions[sessionID] = st
	}
	if in.StepNumber == 0 {
		st.turnStrong = false
		st.turnPrompted = false
		switch ClassifyPrompt(in.Prompt) {
		case TierStrong:
			st.turnStrong, st.turnPrompted = true, true
		case TierFast:
			st.turnPrompted = true
		}
	} else {
		st.sinceSwitch++
	}

	next, reason := r.choose(st, in)
	d := Decision{Tier: next, Reason: reason}
	if next != st.tier {
		d.Switched = true
		st.tier = next
		st.sinceSwitch = 0
	}
	return d
}

// Forget drops a session's state.
func (r *Router) Forget(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
}

func (r *Router) choose(st *state, in Input) (Tier, string) {
	cur := st.tier
	// Hard constraints first: the fast model cannot do this work at all.
	if in.NeedStrong {
		return TierStrong, "turn needs capabilities the fast model lacks"
	}
	if in.FastWindow > 0 && float64(in.ContextTokens) > r.cfg.contextFraction()*float64(in.FastWindow) {
		return TierStrong, "context too large for the fast model"
	}
	// Clear escalation signals override dwell time.
	if errorStreak(in.Steps) >= r.cfg.errorStreak() {
		return TierStrong, "repeated tool failures"
	}
	if testFailure(in.Steps) {
		return TierStrong, "failing test or build output"
	}
	if in.StepNumber == 0 {
		switch {
		case st.turnStrong:
			return TierStrong, "prompt asks for design, debugging or review"
		case st.turnPrompted:
			return TierFast, "prompt asks for reading, searching or a simple edit"
		}
		return cur, "no clear signal, keeping " + string(cur)
	}
	if cur == TierFast {
		return TierFast, "keeping fast"
	}
	// Moving down is conservative: a turn that opened with strong work
	// stays strong, and otherwise it takes a streak of read-only steps, the
	// dwell time, and a saving that outweighs the cache given up.
	if st.turnStrong {
		return TierStrong, "turn opened with strong work"
	}
	if in.CrossProvider {
		// A different provider mid-turn re-encodes every earlier tool call
		// and result; only the start of a turn may do that.
		return TierStrong, "no cross-provider downshift inside a turn"
	}
	if lightStreak(in.Steps) >= r.cfg.downshiftStreak() && st.sinceSwitch >= r.cfg.minDwell() && downshiftPaysOff(in) {
		return TierFast, "run of read-only steps"
	}
	return TierStrong, "keeping strong"
}

// StepCost prices token counts at p.
func StepCost(p Prices, input, output, cacheRead, cacheWrite int64) float64 {
	return p.In/1e6*float64(input) + p.Out/1e6*float64(output) +
		p.CacheRead/1e6*float64(cacheRead) + p.CacheWrite/1e6*float64(cacheWrite)
}
