package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	atlaspolicy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/policy"
)

var (
	policyEngines  sync.Map // session ID -> *atlaspolicy.Engine
	policyAuditDir string
	policyAuditMu  sync.Mutex
)

// SetPolicyAuditDir sets where new policy engines append their audit log.
func SetPolicyAuditDir(dir string) {
	policyAuditMu.Lock()
	defer policyAuditMu.Unlock()
	policyAuditDir = dir
}

// PolicyFor returns the session's shell policy engine, creating it on first
// use. The engine holds the session's user constraints, grants and the
// tracker of downloaded files, so it must outlive a single tool call.
func PolicyFor(sessionID string) *atlaspolicy.Engine {
	if v, ok := policyEngines.Load(sessionID); ok {
		return v.(*atlaspolicy.Engine)
	}
	policyAuditMu.Lock()
	file := ""
	if policyAuditDir != "" {
		file = filepath.Join(policyAuditDir, "policy-audit.jsonl")
	}
	policyAuditMu.Unlock()
	v, _ := policyEngines.LoadOrStore(sessionID, atlaspolicy.New(file))
	return v.(*atlaspolicy.Engine)
}

// currentGitBranch names the checked-out branch, or "" when unknown. It is
// only consulted for commands that push.
func currentGitBranch(ctx context.Context, dir string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return ""
	}
	return branch
}
