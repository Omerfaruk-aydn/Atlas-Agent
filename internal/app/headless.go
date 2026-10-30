package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
)

// Permission policies for an unattended non-interactive run. The empty
// value keeps the historical behavior of "run", which approves every tool
// call; the other values never prompt and deny whatever they do not allow.
const (
	// HeadlessPermissionsConfigured keeps the configured permission mode
	// and allowed-tools list; a call that would need an answer is denied.
	HeadlessPermissionsConfigured = "configured"
	// HeadlessPermissionsReadOnly runs in plan mode: read-only tools only.
	HeadlessPermissionsReadOnly = "readonly"
	// HeadlessPermissionsEdits also approves file edits.
	HeadlessPermissionsEdits = "edits"
	// HeadlessPermissionsAuto approves every tool call.
	HeadlessPermissionsAuto = "auto"
)

// HeadlessOptions tunes the next RunNonInteractive call.
type HeadlessOptions struct {
	// Permissions is one of the HeadlessPermissions* policies. Empty means
	// auto, as "run" has always behaved.
	Permissions string
	// Goal runs the prompt as a goal: the agent keeps taking turns until a
	// verifier agrees it is met or its budget runs out.
	Goal bool
}

// ValidHeadlessPermissions reports whether policy names a known policy.
func ValidHeadlessPermissions(policy string) bool {
	switch policy {
	case "", HeadlessPermissionsConfigured, HeadlessPermissionsReadOnly, HeadlessPermissionsEdits, HeadlessPermissionsAuto:
		return true
	}
	return false
}

// SetHeadless sets the unattended-run options used by RunNonInteractive.
func (app *App) SetHeadless(opts HeadlessOptions) { app.headless = opts }

// applyHeadlessPermissions installs the run's permission policy and returns
// the context the agent must run under.
func (app *App) applyHeadlessPermissions(ctx context.Context, sessionID string) (context.Context, error) {
	switch app.headless.Permissions {
	case "", HeadlessPermissionsAuto:
		app.Permissions.AutoApproveSession(sessionID)
		return ctx, nil
	case HeadlessPermissionsConfigured:
	case HeadlessPermissionsReadOnly:
		app.Permissions.SetMode(permission.ModePlan)
	case HeadlessPermissionsEdits:
		app.Permissions.SetMode(permission.ModeAutoAcceptEdits)
	default:
		return ctx, fmt.Errorf("unknown permissions policy %q", app.headless.Permissions)
	}
	return permission.WithoutPrompts(ctx), nil
}

// goalPollInterval is how often a goal run checks whether its goal has
// ended.
const goalPollInterval = 500 * time.Millisecond

// finishGoalRun waits for a goal run to end. The coordinator takes a goal's
// later turns in the background after the first Run returns, so an
// unattended caller has to wait for the session to settle before it reports.
// The closing assistant message is then written out, because the turns taken
// meanwhile were not streamed.
func (app *App) finishGoalRun(ctx context.Context, sessionID string, output io.Writer, jsonOutput bool, collected *strings.Builder) error {
	ticker := time.NewTicker(goalPollInterval)
	defer ticker.Stop()
	for {
		sess, err := app.Sessions.Get(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("failed to check goal progress: %w", err)
		}
		if sess.Goal == "" && !app.AgentCoordinator.IsSessionBusy(sessionID) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	last, err := app.Messages.GetLastAssistantMessage(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("failed to read the goal result: %w", err)
	}
	final := strings.TrimSpace(last.Content().String())
	if jsonOutput {
		collected.Reset()
		collected.WriteString(final)
		return nil
	}
	_, _ = fmt.Fprintf(output, "\n\n%s", final)
	return nil
}
