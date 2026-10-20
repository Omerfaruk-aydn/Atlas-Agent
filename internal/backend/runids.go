package backend

import (
	"errors"
	"time"
)

// ErrRunAlreadyAccepted is returned by [Backend.SendMessage] when a
// prompt carries a RunID the workspace accepted recently. The first
// request's run is the one that executes; the HTTP layer reports the
// retry as accepted without starting anything.
var ErrRunAlreadyAccepted = errors.New("run already accepted")

// runIDRetention is how long an accepted RunID is remembered. It only has
// to outlast a client's retry window after a lost acknowledgement.
const runIDRetention = 15 * time.Minute

// maxRememberedRunIDs bounds the memory spent on retry detection. When
// it is reached the oldest entries are dropped first.
const maxRememberedRunIDs = 4096

// claimRunID records id as accepted and reports whether this call was
// the first to do so within the retention window.
func (w *Workspace) claimRunID(id string, now time.Time) bool {
	w.runIDsMu.Lock()
	defer w.runIDsMu.Unlock()
	if w.runIDs == nil {
		w.runIDs = make(map[string]time.Time)
	}
	if at, ok := w.runIDs[id]; ok && now.Sub(at) < runIDRetention {
		return false
	}
	if len(w.runIDs) >= maxRememberedRunIDs {
		w.pruneRunIDsLocked(now)
	}
	w.runIDs[id] = now
	return true
}

// pruneRunIDsLocked drops expired entries and, if the map is still full,
// the oldest ones. Callers hold runIDsMu.
func (w *Workspace) pruneRunIDsLocked(now time.Time) {
	for id, at := range w.runIDs {
		if now.Sub(at) >= runIDRetention {
			delete(w.runIDs, id)
		}
	}
	for len(w.runIDs) >= maxRememberedRunIDs {
		var oldestID string
		var oldest time.Time
		for id, at := range w.runIDs {
			if oldestID == "" || at.Before(oldest) {
				oldestID, oldest = id, at
			}
		}
		delete(w.runIDs, oldestID)
	}
}
