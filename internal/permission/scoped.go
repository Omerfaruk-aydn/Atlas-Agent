package permission

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// GrantScope controls how long an explicit runtime elevation remains valid.
type GrantScope string

const (
	GrantOnce    GrantScope = "call"
	GrantTurn    GrantScope = "turn"
	GrantSession GrantScope = "session"
	turnGrantTTL            = 30 * time.Minute
)

type scopedGrant struct {
	expires time.Time
	once    bool
}

// ScopedService is implemented by the runtime permission service. It is
// separate from Service so existing remote and test implementations remain
// compatible.
type ScopedService interface {
	GrantScoped(sessionID, toolName, action, path string, scope GrantScope) error
}

type scopedGrants struct {
	mu     sync.Mutex
	grants map[PermissionKey]scopedGrant
}

func (s *scopedGrants) grant(key PermissionKey, scope GrantScope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope == GrantSession {
		return fmt.Errorf("session grants use the persistent permission store")
	}
	s.grants[key] = scopedGrant{once: scope == GrantOnce, expires: time.Now().Add(turnGrantTTL)}
	return nil
}

func (s *scopedGrants) allowed(key PermissionKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, ok := s.grants[key]
	if !ok {
		return false
	}
	if time.Now().After(grant.expires) || grant.once {
		delete(s.grants, key)
	}
	return time.Now().Before(grant.expires)
}

// GrantScoped grants a bounded permission after the user approved the
// request_permission tool. Paths are canonicalized to avoid alias bypasses.
func (s *permissionService) GrantScoped(sessionID, toolName, action, path string, scope GrantScope) error {
	if sessionID == "" || toolName == "" || action == "" {
		return fmt.Errorf("session, tool and action are required")
	}
	if scope != GrantOnce && scope != GrantTurn && scope != GrantSession {
		return fmt.Errorf("unsupported grant scope %q", scope)
	}
	if path == "" {
		path = s.workingDir
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	key := PermissionKey{SessionID: sessionID, ToolName: toolName, Action: action, Path: absolute}
	if scope == GrantSession {
		s.sessionPermissions.Set(key, true)
		return nil
	}
	return s.scoped.grant(key, scope)
}
