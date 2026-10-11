package nodes

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

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

