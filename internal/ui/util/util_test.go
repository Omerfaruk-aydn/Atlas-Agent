package util

import "testing"

// A change that actually took effect (goal set, snippet saved, session
// reconnected) is not the same event as a passing tip, and must not
// render identically to one -- that is what made a real confirmation
// look, from the outside, like nothing had happened at all.
func TestNewSuccessMsgIsDistinctFromInfo(t *testing.T) {
	if NewSuccessMsg("done").Type != InfoTypeSuccess {
		t.Fatalf("NewSuccessMsg must produce InfoTypeSuccess, not %v", NewSuccessMsg("done").Type)
	}
	if NewInfoMsg("done").Type == InfoTypeSuccess {
		t.Fatal("NewInfoMsg must not be mistaken for a success message")
	}
}
