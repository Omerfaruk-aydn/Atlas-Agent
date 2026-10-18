package workplan

import (
	"fmt"
	"slices"
)

// Weakened lists the ways after loosens the verification of before: a
// contract check removed or replaced, a feature's own check removed, or a
// feature marked done dropped from the plan. Adding checks, rewording titles
// and marking features done are progress, not weakening. A nil before (a new
// plan) weakens nothing.
func Weakened(before, after *Plan) []string {
	if before == nil || after == nil {
		return nil
	}
	var out []string
	kept := make([]string, 0, len(after.Verify))
	for _, check := range after.Verify {
		kept = append(kept, check.Command)
	}
	for _, check := range before.Verify {
		if !slices.Contains(kept, check.Command) {
			out = append(out, fmt.Sprintf("contract check `%s` was removed or changed", clip(check.Command, maxSummaryCommand)))
		}
	}
	for _, old := range before.Features {
		feature, ok := after.Feature(old.ID)
		switch {
		case !ok && old.Done:
			out = append(out, fmt.Sprintf("feature %s, already done, was removed from the plan", old.ID))
		case !ok:
		case old.Verify != "" && feature.Verify == "":
			out = append(out, fmt.Sprintf("feature %s lost its check `%s`", old.ID, clip(old.Verify, maxSummaryCommand)))
		case old.Verify != "" && feature.Verify != old.Verify:
			out = append(out, fmt.Sprintf("feature %s's check changed from `%s` to `%s`", old.ID,
				clip(old.Verify, maxSummaryCommand), clip(feature.Verify, maxSummaryCommand)))
		case old.Acceptance != "" && feature.Acceptance == "":
			out = append(out, fmt.Sprintf("feature %s lost its acceptance criterion", old.ID))
		}
	}
	return out
}
