package nodes

import (
	"context"
	"net/http"
	"slices"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
)

// Need is the permission one API route requires. Paired-device nodes map
// needs onto their coarse capabilities; a session-scoped Access (remote
// support) maps them onto whatever its owner granted.
type Need string

const (
	// NeedInfo reads the node's identity and the caller's own grant.
	NeedInfo Need = "info"
	// NeedOps reads or cancels the caller's own journaled operations.
	NeedOps Need = "ops"
	// NeedSystemInfo collects read-only diagnostics.
	NeedSystemInfo Need = "system_info"
	// NeedExec runs a literal argv.
	NeedExec Need = "exec"
	// NeedFileList lists a directory under an allowed root.
	NeedFileList Need = "file_list"
	// NeedFileRead downloads a file from the node.
	NeedFileRead Need = "file_read"
	// NeedFileWrite uploads a new file to the node.
	NeedFileWrite Need = "file_write"
	// NeedFileModify replaces an existing file on the node.
	NeedFileModify Need = "file_modify"
	// NeedDesktopView captures the screen and reads accessibility trees.
	NeedDesktopView Need = "desktop_view"
	// NeedDesktopInput sends mouse and keyboard input.
	NeedDesktopInput Need = "desktop_input"
	// NeedDesktopLaunch starts a visible program in the desktop session.
	NeedDesktopLaunch Need = "desktop_launch"
	// NeedBrowser reads a support-owned browser session.
	NeedBrowser Need = "browser_view"
	// NeedBrowserInput changes that browser session.
	NeedBrowserInput Need = "browser_input"
)

// legacyCapability is the coarse capability a paired-device node checks.
// The empty capability needs only authentication.
func legacyCapability(n Need) Capability {
	switch n {
	case NeedExec, NeedSystemInfo:
		return CapExec
	case NeedFileList, NeedFileRead, NeedFileWrite, NeedFileModify:
		return CapFiles
	case NeedDesktopView, NeedDesktopInput, NeedDesktopLaunch:
		return CapDesktop
	}
	return ""
}

// Root is a labeled directory a grant may use. Paths in requests start with
// the label ("Desktop/report.pdf"); a grant without roots uses the node root
// directly.
type Root struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// Grant is what one authenticated request may do.
type Grant struct {
	Device Device
	// SessionID and Generation stamp every operation and observation, so a
	// result is never attributed to another session or connection.
	SessionID  string
	Generation uint64
	Roots      []Root
	// Permissions are the fine-grained names shown to the controller.
	Permissions []string
}

// Access replaces the paired-device bearer check for nodes that grant
// session-scoped access. Authorize runs before every authenticated route
// with the route's need; a non-nil error is sent to the caller unchanged
// and nothing else runs.
type Access interface {
	Authorize(r *http.Request, need Need) (Grant, *APIError, int)
}

// ExecPolicy optionally refines an exec or a launch (kind "exec" or
// "launch") after authorization, for example to require an install
// permission for an installer command.
type ExecPolicy interface {
	CheckExec(g Grant, kind string, argv []string) *APIError
}

// OperationObserver optionally sees every journaled operation as it starts
// and finishes, for an owner-facing activity view and history.
type OperationObserver interface {
	OperationStarted(g Grant, op Operation)
	OperationFinished(g Grant, op Operation)
}

type grantKey struct{}

func grantFrom(r *http.Request) Grant {
	g, _ := r.Context().Value(grantKey{}).(Grant)
	return g
}

func withGrant(r *http.Request, g Grant) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), grantKey{}, g))
}

// sortedRoots returns roots in label order for stable defaults.
func sortedRoots(roots []Root) []Root {
	out := slices.Clone(roots)
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// resolveFor maps a request path onto the grant's roots, or onto the node
// root for a grant without roots. It returns the absolute path and the root
// it belongs to. Paths may be label-relative or absolute inside a root (see
// matchRoot). Traversal and symlink escapes are refused by transfer.Resolve
// against the selected root.
func (s *Server) resolveFor(g Grant, rel string) (root, full string, err error) {
	if len(g.Roots) == 0 {
		full, err = transfer.Resolve(s.opts.Root, rel)
		return s.opts.Root, full, err
	}
	r, rest, err := matchRoot(g.Roots, rel)
	if err != nil {
		return "", "", err
	}
	if rest == "" || rest == "." {
		return r.Path, r.Path, nil
	}
	full, err = transfer.Resolve(r.Path, rest)
	return r.Path, full, err
}

// splitFor returns the root and the root-relative remainder of rel, for
// the transfer functions that take (root, rel).
func (s *Server) splitFor(g Grant, rel string) (root, inner string, err error) {
	if len(g.Roots) == 0 {
		return s.opts.Root, rel, nil
	}
	r, rest, err := matchRoot(g.Roots, rel)
	if err != nil {
		return "", "", err
	}
	if rest == "" || rest == "." {
		return "", "", &transfer.Error{Code: "invalid_path", Message: "path must name a file inside the allowed folder " + r.Label}
	}
	return r.Path, rest, nil
}

// defaultDir is the working directory for commands and launches.
func (s *Server) defaultDir(g Grant) string {
	if len(g.Roots) == 0 {
		return s.opts.Root
	}
	return sortedRoots(g.Roots)[0].Path
}
