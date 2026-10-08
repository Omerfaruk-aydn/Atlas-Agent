package agent

import (
	"net/http"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// modelChain tracks, for the lifetime of one Run, which model in a
// configured fallback list is currently serving a role. It exists because
// the retry middleware only ever retries the same model: a provider that
// answers 429 (rate limit or quota exhausted) keeps answering 429 on every
// retry, so without something to hand the next attempt to a different
// provider, a quota outage on the primary is a dead turn even when a
// perfectly usable fallback is configured.
//
// A chain is scoped to a single Run call, not shared across turns: quota
// limits are frequently transient (a burst limit, not an outage), so each
// new turn is worth one attempt against the primary before falling back
// again.
type modelChain struct {
	models []Model // models[0] is the primary; the rest are the configured fallbacks, in order.
	active int     // index into models of the model currently in use.
}

// newModelChain builds a chain of primary followed by fallbacks, starting
// at startIndex -- 0 for the primary, or a sticky fallback's remembered
// index when its cooldown has not yet expired. An out-of-range startIndex
// (stale state from a fallback list that has since shrunk) is treated as 0
// rather than panicking or silently clamping to the last model.
func newModelChain(primary Model, fallbacks []Model, startIndex int) *modelChain {
	models := append([]Model{primary}, fallbacks...)
	if startIndex < 0 || startIndex >= len(models) {
		startIndex = 0
	}
	return &modelChain{models: models, active: startIndex}
}

// stickyFallback remembers which model a chain last failed over to, and
// until when that choice should stick, so a rate-limit cooldown can survive
// across separate Run calls instead of always retrying the primary model on
// the very next turn.
type stickyFallback struct {
	index int
	until time.Time
}

// activeIndex is the chain index a new Run should start at: index while the
// cooldown has not yet expired, otherwise back to the primary (0).
func (s stickyFallback) activeIndex(now time.Time) int {
	if s.until.IsZero() || !now.Before(s.until) {
		return 0
	}
	return s.index
}

// Current is the model presently in use.
func (c *modelChain) Current() Model {
	return c.models[c.active]
}

// RefreshModels replaces clients rebuilt after credential renewal without
// changing this turn's selected model, fallback order, or active index.
func (c *modelChain) RefreshModels(primary Model, fallbacks []Model) {
	candidates := append([]Model{primary}, fallbacks...)
	for i, current := range c.models {
		for _, candidate := range candidates {
			if candidate.Model != nil && current.ModelCfg.Provider == candidate.ModelCfg.Provider && current.ModelCfg.Model == candidate.ModelCfg.Model {
				c.models[i].Model = candidate.Model
				break
			}
		}
	}
}

// Provider adapts Current for fantasy's ModelProvider hook, which is called
// fresh on every retry attempt -- including the first -- so a chain that has
// advanced by the time of a later attempt hands that attempt the new model
// without anything else needing to know.
func (c *modelChain) Provider() fantasy.LanguageModel {
	return c.Current().Model
}

// HandleRetry inspects a failure that fantasy is about to retry and, if it
// is a 429 (rate limit or quota) and a further model is configured, moves
// the chain to it. It reports whether it moved, so the caller can log the
// change; once moved, the chain does not move back to an earlier model
// within this Run even if that model would also fail -- it simply continues
// down the list on repeated 429s.
func (c *modelChain) HandleRetry(err *fantasy.ProviderError) bool {
	if err == nil || err.StatusCode != http.StatusTooManyRequests {
		return false
	}
	if c.active+1 >= len(c.models) {
		return false
	}
	c.active++
	return true
}

// Fellback reports whether the chain has moved off the primary model.
func (c *modelChain) Fellback() bool {
	return c.active > 0
}

// isRateLimited reports whether err is the 429 (rate limit / quota) status
// HandleRetry acts on. Exported to agent.go so it does not need its own
// net/http import just to check a status code.
func isRateLimited(err *fantasy.ProviderError) bool {
	return err != nil && err.StatusCode == http.StatusTooManyRequests
}
