package permission

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/google/uuid"
)

// hookApprovalKey is the unexported context key used to mark a tool call as
// pre-approved by a PreToolUse hook. The value is the tool call ID so an
// approval can't be reused across calls that happen to share a context.
type hookApprovalKey struct{}

// WithHookApproval returns a context that marks the given tool call ID as
// pre-approved by a hook. When the permission service sees a matching
// request it short-circuits the normal prompt and grants immediately.
func WithHookApproval(ctx context.Context, toolCallID string) context.Context {
	return context.WithValue(ctx, hookApprovalKey{}, toolCallID)
}

// hookApproved reports whether the context carries a hook approval for the
// given tool call ID.
func hookApproved(ctx context.Context, toolCallID string) bool {
	if toolCallID == "" {
		return false
	}
	v, _ := ctx.Value(hookApprovalKey{}).(string)
	return v == toolCallID
}

// PermissionMode is the current session-wide permission policy, cycled by
// the user at runtime (Ctrl+Y and the mode-cycle keybinding). It sits above
// the five legacy override layers in Request(): Bypass and Plan are decided
// before any of them run, so neither a stale sessionPermissions entry nor a
// atlasrc allowlist can leak a mutating call through Plan mode.
type PermissionMode string

const (
	// ModeManual is today's default: every mutating/risky tool call goes
	// through the full allowlist/hook/session-cache/dialog pipeline.
	ModeManual PermissionMode = "manual"
	// ModeAutoAcceptEdits silently approves file read/write/edit tool
	// calls (see agent/tools.CategoryEdit) but still runs the full manual
	// pipeline for shell, network, and MCP calls.
	ModeAutoAcceptEdits PermissionMode = "auto_accept_edits"
	// ModePlan is read-only: any call that isn't in
	// agent/tools.CategoryReadOnly is denied outright, including shell
	// commands that would otherwise skip the prompt via the "safe
	// command" allowlist.
	ModePlan PermissionMode = "plan"
	// ModeBypass skips all confirmation. Equivalent to the pre-existing
	// --yolo / SkipRequests(true) behavior.
	ModeBypass PermissionMode = "bypass"
)

// ExitPlanModeToolName is the one tool name ModePlan lets reach the normal
// confirmation dialog instead of being denied outright. It's how the agent
// asks the user, from inside the conversation, to leave plan mode and start
// implementing the plan it just presented — the dialog approval itself is
// the user's mode switch. See internal/agent/tools/exit_plan_mode.go, whose
// ExitPlanModeToolName constant must stay equal to this one.
const ExitPlanModeToolName = "exit_plan_mode"

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	// Safe marks a call the tool itself considers low-risk (e.g. bash's
	// read-only command allowlist). ModeManual/ModeAutoAcceptEdits grant
	// it immediately, same as today's pre-Request short-circuit; ModePlan
	// still denies it — plan mode's read-only guarantee must hold even
	// for calls the tool itself thought were harmless.
	Safe bool `json:"-"`
}

type PermissionNotification struct {
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted"`
	Denied     bool   `json:"denied"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type Service interface {
	pubsub.Subscriber[PermissionRequest]
	// GrantPersistent grants a permission request and remembers the grant
	// for the session. It returns true if this call actually resolved the
	// pending request; false if the request had already been resolved
	// (e.g., by another concurrent caller) or is unknown.
	GrantPersistent(permission PermissionRequest) bool
	// Grant grants a permission request. It returns true if this call
	// actually resolved the pending request; false if the request had
	// already been resolved or is unknown.
	Grant(permission PermissionRequest) bool
	// Deny denies a permission request. It returns true if this call
	// actually resolved the pending request; false if the request had
	// already been resolved or is unknown.
	Deny(permission PermissionRequest) bool
	Request(ctx context.Context, opts CreatePermissionRequest) (bool, error)
	AutoApproveSession(sessionID string)
	SetSkipRequests(skip bool)
	SkipRequests() bool
	// Mode returns the current PermissionMode.
	Mode() PermissionMode
	// SetMode sets the current PermissionMode. Overwrites whatever
	// SetSkipRequests previously set (and vice versa) since both act on
	// the same underlying state.
	SetMode(mode PermissionMode)
	// SetAllowedTools replaces the tool/command allowlist in place, so a
	// chat-driven change to permissions.allowed_tools takes effect
	// immediately on an already-running session instead of requiring a
	// restart.
	SetAllowedTools(allowedTools []string)
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification]
}

// PermissionKey is a composite key for session permission lookups.
type PermissionKey struct {
	SessionID string
	ToolName  string
	Action    string
	Path      string
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	notificationBroker    *pubsub.Broker[PermissionNotification]
	workingDir            string
	sessionPermissions    *csync.Map[PermissionKey, bool]
	pendingRequests       *csync.Map[string, chan bool]
	autoApproveSessions   map[string]bool
	autoApproveSessionsMu sync.RWMutex
	mode                  PermissionMode
	modeMu                sync.RWMutex
	// allowedTools is *csync.Slice, not a plain slice (which the old code
	// read here without any lock, a data race with a concurrent config
	// reload), so that a chat-driven change to permissions.allowed_tools
	// is both safe to read concurrently and takes effect immediately --
	// see SetAllowedTools.
	allowedTools *csync.Slice[string]

	// used to make sure we only process one request at a time
	requestMu       sync.Mutex
	activeRequest   *PermissionRequest
	activeRequestMu sync.Mutex
}

// resolve atomically removes the pending request entry for the given
// permission and, if it was still pending, publishes exactly one
// PermissionNotification and forwards the outcome to the waiter on
// respCh. It returns true if this call resolved the request, false if
// it had already been resolved (e.g., by another concurrent caller) or
// the request ID is unknown.
//
// If onResolve is non-nil it runs after the pending entry has been
// taken but before the notification is published or the waiter is
// unblocked. This lets GrantPersistent record the session permission
// only when it actually wins the race, so a losing GrantPersistent
// that lost to a Deny does not leak an auto-approve entry.
//
// All three public resolution methods (Grant, GrantPersistent, Deny)
// route through this helper so multi-subscriber UIs can race safely:
// the first caller wins, the rest become no-ops.
func (s *permissionService) resolve(permission PermissionRequest, granted, denied bool, onResolve func()) bool {
	respCh, ok := s.pendingRequests.Take(permission.ID)
	if !ok {
		return false
	}

	if onResolve != nil {
		onResolve()
	}

	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    granted,
		Denied:     denied,
	})

	// respCh is buffered (cap 1) and only ever has at most one sender
	// per request because Take removes the entry under the map lock,
	// so this send never blocks.
	respCh <- granted

	s.activeRequestMu.Lock()
	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
	s.activeRequestMu.Unlock()
	return true
}

func (s *permissionService) GrantPersistent(permission PermissionRequest) bool {
	// Record the persistent grant only if this call wins the
	// pending-request race. Otherwise a losing GrantPersistent that
	// lost to a Deny would still leave an auto-approve entry behind,
	// silently flipping later denied calls to allowed.
	return s.resolve(permission, true, false, func() {
		s.sessionPermissions.Set(PermissionKey{
			SessionID: permission.SessionID,
			ToolName:  permission.ToolName,
			Action:    permission.Action,
			Path:      permission.Path,
		}, true)
	})
}

func (s *permissionService) Grant(permission PermissionRequest) bool {
	return s.resolve(permission, true, false, nil)
}

func (s *permissionService) Deny(permission PermissionRequest) bool {
	return s.resolve(permission, false, true, nil)
}

func (s *permissionService) Request(ctx context.Context, opts CreatePermissionRequest) (bool, error) {
	mode := s.Mode()

	// Bypass and Plan are decided before any of the five legacy override
	// layers below: Bypass because "skip everything" should mean exactly
	// that, and Plan because its read-only guarantee must be
	// unconditional — a stale sessionPermissions grant or a atlasrc
	// allowlist entry recorded before switching into Plan mode must not
	// be able to leak a mutating call through.
	switch mode {
	case ModeBypass:
		return true, nil
	case ModePlan:
		if CategoryForTool(opts.ToolName) == CategoryReadOnly {
			return true, nil
		}
		if opts.ToolName == ExitPlanModeToolName {
			// The only way out of plan mode from inside the conversation:
			// let this one reach the normal dialog below instead of being
			// denied outright.
			break
		}
		// Deny outright: no dialog, no channel wait. opts.Safe (bash's
		// "this command is read-only") does NOT grant an exception here
		// — plan mode blocks even nominally-safe commands.
		return false, nil
	case ModeAutoAcceptEdits:
		if CategoryForTool(opts.ToolName) == CategoryEdit {
			return true, nil
		}
		// Non-edit tools (bash, network, MCP) fall through to the full
		// manual pipeline below, unchanged.
	}

	// ModeManual (and ModeAutoAcceptEdits for non-edit tools): a call the
	// tool itself flagged as safe (bash's read-only allowlist) is granted
	// immediately, matching today's pre-Request short-circuit exactly.
	if opts.Safe {
		return true, nil
	}

	// Check if the tool/action combination is in the allowlist
	commandKey := opts.ToolName + ":" + opts.Action
	allowedTools := s.allowedTools.Copy()
	if slices.Contains(allowedTools, commandKey) || slices.Contains(allowedTools, opts.ToolName) {
		return true, nil
	}

	// A PreToolUse hook that returned decision=allow stamps the context
	// with the tool call ID. Treat that as a pre-approval and skip the
	// prompt entirely. We still publish a granted notification so the UI
	// and audit subscribers see the outcome.
	if hookApproved(ctx, opts.ToolCallID) {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	// tell the UI that a permission was requested
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: opts.ToolCallID,
	})

	s.autoApproveSessionsMu.RLock()
	autoApprove := s.autoApproveSessions[opts.SessionID]
	s.autoApproveSessionsMu.RUnlock()

	if autoApprove {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	fileInfo, err := os.Stat(opts.Path)
	dir := opts.Path
	if err == nil {
		if fileInfo.IsDir() {
			dir = opts.Path
		} else {
			dir = filepath.Dir(opts.Path)
		}
	}

	if dir == "." {
		dir = s.workingDir
	}
	permission := PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}

	if _, ok := s.sessionPermissions.Get(PermissionKey{
		SessionID: permission.SessionID,
		ToolName:  permission.ToolName,
		Action:    permission.Action,
		Path:      permission.Path,
	}); ok {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, nil
	}

	s.activeRequestMu.Lock()
	s.activeRequest = &permission
	s.activeRequestMu.Unlock()

	respCh := make(chan bool, 1)
	s.pendingRequests.Set(permission.ID, respCh)
	defer s.pendingRequests.Del(permission.ID)

	// Publish the request
	s.Publish(pubsub.CreatedEvent, permission)

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case granted := <-respCh:
		return granted, nil
	}
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessionsMu.Lock()
	s.autoApproveSessions[sessionID] = true
	s.autoApproveSessionsMu.Unlock()
}

func (s *permissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification] {
	return s.notificationBroker.Subscribe(ctx)
}

// SetSkipRequests is a compatibility shim over SetMode: true seeds
// ModeBypass, false drops back to ModeManual (only if currently bypassed —
// it never overwrites Plan/AutoAcceptEdits, matching the old bool's
// "on/off" semantics without clobbering the newer modes).
func (s *permissionService) SetSkipRequests(skip bool) {
	if skip {
		s.SetMode(ModeBypass)
		return
	}
	if s.Mode() == ModeBypass {
		s.SetMode(ModeManual)
	}
}

// SkipRequests is a compatibility shim over Mode.
func (s *permissionService) SkipRequests() bool {
	return s.Mode() == ModeBypass
}

func (s *permissionService) Mode() PermissionMode {
	s.modeMu.RLock()
	defer s.modeMu.RUnlock()
	return s.mode
}

func (s *permissionService) SetMode(mode PermissionMode) {
	s.modeMu.Lock()
	s.mode = mode
	s.modeMu.Unlock()
}

// SetAllowedTools replaces the tool/command allowlist in place, so a
// chat-driven change to permissions.allowed_tools takes effect on the very
// next Request call instead of requiring a restart.
func (s *permissionService) SetAllowedTools(allowedTools []string) {
	s.allowedTools.SetSlice(allowedTools)
}

func NewPermissionService(workingDir string, skip bool, allowedTools []string) Service {
	mode := ModeManual
	if skip {
		mode = ModeBypass
	}
	svc := &permissionService{
		Broker:              pubsub.NewBroker[PermissionRequest](),
		notificationBroker:  pubsub.NewBroker[PermissionNotification](),
		workingDir:          workingDir,
		sessionPermissions:  csync.NewMap[PermissionKey, bool](),
		autoApproveSessions: make(map[string]bool),
		allowedTools:        csync.NewSliceFrom(allowedTools),
		pendingRequests:     csync.NewMap[string, chan bool](),
		mode:                mode,
	}
	return svc
}
