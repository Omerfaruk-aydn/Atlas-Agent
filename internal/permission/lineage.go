package permission

import "sync"

// maxLineageDepth bounds the walk from a session to its root, so a corrupt
// parent loop can never spin.
const maxLineageDepth = 16

// lineage remembers which session started which, so a sub-agent's session
// shares the "allow for this session" approvals of the session that
// delegated to it. Without it every delegated run is a brand new session and
// asks again for what the user has already allowed.
type lineage struct {
	mu      sync.RWMutex
	parents map[string]string
}

// Link records that child was started by parent.
func (l *lineage) Link(child, parent string) {
	if child == "" || parent == "" || child == parent {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.parents == nil {
		l.parents = map[string]string{}
	}
	l.parents[child] = parent
}

// Root returns the session at the top of id's chain, or id itself when it was
// not started by another session.
func (l *lineage) Root(id string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for range maxLineageDepth {
		parent, ok := l.parents[id]
		if !ok {
			return id
		}
		id = parent
	}
	return id
}

// Forget drops a finished session from the chain.
func (l *lineage) Forget(child string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.parents, child)
}

// SessionLinker is implemented by a permission service that can share
// session-wide approvals between a session and the sub-agents it starts.
type SessionLinker interface {
	// LinkSession records that child was started by parent.
	LinkSession(child, parent string)
	// UnlinkSession forgets child once its run is over.
	UnlinkSession(child string)
}

// LinkSession implements SessionLinker.
func (s *permissionService) LinkSession(child, parent string) { s.lineage.Link(child, parent) }

// UnlinkSession implements SessionLinker.
func (s *permissionService) UnlinkSession(child string) { s.lineage.Forget(child) }
