package browser

import (
	"encoding/json"
	"fmt"
	"time"
)

// Bounds for waiting on a semantic target that cannot be acted on. Waiting
// only helps when the page is still changing; an absent container, an
// absent target nobody asked to wait for, or a control behind the same
// blocker do not fix themselves by polling longer.
const (
	// WithinAbsentGrace bounds waiting for a within container that does not
	// exist. Waiting for a container to appear is a wait_for on the
	// container itself.
	WithinAbsentGrace = 2 * time.Second
	// TargetAbsentGrace bounds waiting for an absent target when the call
	// did not ask to wait (no timeout_ms).
	TargetAbsentGrace = 2 * time.Second
	// BlockerStableGrace bounds waiting while the same element keeps
	// covering, disabling or hiding the target.
	BlockerStableGrace = 2500 * time.Millisecond
)

// resolutionWaits tracks how long one unchanged reason has kept a semantic
// action from passing.
type resolutionWaits struct {
	action        string
	explicitWait  bool
	reason        string
	since         time.Time
	lastSignature string
}

func newResolutionWaits(now time.Time, p Request) *resolutionWaits {
	return &resolutionWaits{action: p.Action, explicitWait: p.TimeoutMS > 0 && !p.internalClick(), since: now}
}

// internalClick reports a click Atlas issued for a ref or selector, whose
// timeout is the configured action timeout rather than a request to wait.
func (p Request) internalClick() bool {
	return p.Action == "semantic_click" && p.Selector != "" && p.Name == "" && p.Role == "" && p.Label == ""
}

// check returns an error once one reason has persisted past its grace.
// Every new reason or blocker restarts the clock.
func (w *resolutionWaits) check(now time.Time, v map[string]any) error {
	reason, _ := v["reason"].(string)
	count, _ := v["count"].(float64)
	blocker, _ := v["blocker"].(string)
	signature := ""
	switch {
	case reason == "within_not_found":
		signature = "within"
	case count == 0 && w.action != "assert":
		signature = "absent"
	case count == 1 && blocker != "" && blocker != "moving" && w.action != "assert":
		signature = "blocked:" + blocker
	}
	if signature != w.lastSignature {
		w.lastSignature, w.since = signature, now
		return nil
	}
	held := now.Sub(w.since)
	switch {
	case signature == "within" && held >= WithinAbsentGrace:
		available, _ := json.Marshal(v["available"])
		return fmt.Errorf("within_not_found: no visible container is named like within after %dms; nothing was sent. Visible named containers: %s. Use one of them (plain label or the role \"label\" form from find/context), or wait_for the container itself if it is still opening", held.Milliseconds(), available)
	case signature == "absent" && !w.explicitWait && held >= TargetAbsentGrace:
		return fmt.Errorf("target_not_found: no visible element matches after %dms; nothing was sent. Observe or find the target and use its current role/name or ref; pass timeout_ms only to wait for something that is still appearing", held.Milliseconds())
	case len(signature) > 8 && signature[:8] == "blocked:" && held >= BlockerStableGrace:
		target, _ := json.Marshal(v["target"])
		return fmt.Errorf("target_not_actionable: %s for %dms; nothing was sent. Target %s. Close or wait for what covers it (wait_for hidden on that element), or choose another route; do not repeat the same call", blocker, held.Milliseconds(), target)
	}
	return nil
}
