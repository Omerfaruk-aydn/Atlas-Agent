package agent

// shouldAutoSummarize reports whether a session that has used tokens of a
// contextWindow-sized window is close enough to the end of it to summarize.
//
// With usedRatio unset (zero, or out of range) it keeps the built-in
// behaviour: a fixed buffer for windows large enough that a proportional one
// would be wastefully big, and a proportion of the window for smaller ones.
// A configured usedRatio replaces both -- summarize once that fraction of the
// window is used.
//
// A contextWindow of 0 means "unknown" (custom and local models often report
// nothing), and never summarizes: truncating a session on a guess is worse
// than letting the provider reject an oversized request.
func shouldAutoSummarize(contextWindow, tokens int64, usedRatio float64) bool {
	if contextWindow <= 0 {
		return false
	}
	if validUsedRatio(usedRatio) {
		return float64(tokens) >= float64(contextWindow)*usedRatio
	}

	return tokens >= compactionPoint(contextWindow, 0)
}

// largeContextWindowCompactAt is the fraction of a large window at which
// the session is summarized even though room remains.
const largeContextWindowCompactAt = 0.75

// validUsedRatio rejects values that would make the agent summarize on every
// turn (<= 0) or never (>= 1), falling back to the built-in thresholds rather
// than honouring a setting that can only misbehave.
func validUsedRatio(r float64) bool {
	return r > 0 && r < 1
}

// costCompactionCeiling caps the built-in compaction point. Every step of a
// session re-reads the whole conversation, so the bill grows with context
// length times step count while the model's grip on early details weakens;
// a session that compacts once around this size and then grows append-only
// costs far less than one that carries several hundred thousand tokens.
// Windows smaller than this keep their proportional point.
const costCompactionCeiling = 120_000
