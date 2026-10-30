package agent

// reviewStatus is the outcome of an independent review. A review that could
// not run, or that returned something unusable, is not a passing review; the
// work may still move on, but it is never reported as verified.
type reviewStatus string

const (
	reviewPassed        reviewStatus = "passed"
	reviewFailed        reviewStatus = "failed"
	reviewUnavailable   reviewStatus = "unavailable"
	reviewInvalidReport reviewStatus = "invalid_report"
	// reviewWaived means no reviewer is configured; the absence is a
	// deliberate setting, not a failure of a required review.
	reviewWaived reviewStatus = "waived"
)

// Verified reports whether the status supports a "verified" completion.
func (s reviewStatus) Verified() bool { return s == reviewPassed }

// Blocking reports whether the status must send the work back.
func (s reviewStatus) Blocking() bool { return s == reviewFailed }
