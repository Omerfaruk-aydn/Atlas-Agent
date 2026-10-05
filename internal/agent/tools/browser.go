package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
)

const BrowserToolName = "browser"

//go:embed browser.md.tpl
var browserDescriptionTmpl string

var browserDescriptionTpl = template.Must(
	template.New("browserDescription").
		Parse(browserDescriptionTmpl),
)

// browserDescriptionData tells the description which browser the model is
// about to drive. Whether the tab arrives carrying the user's own logins
// changes what the model should do when a page looks signed out, and it
// has no way to know unless it is told.
type browserDescriptionData struct {
	RealProfile bool
}

func browserDescription(realProfile bool) string {
	var out strings.Builder
	if err := browserDescriptionTpl.Execute(&out, browserDescriptionData{RealProfile: realProfile}); err != nil {
		// The template is a compiled-in constant; a failure here is a
		// build-time mistake, not a runtime condition to handle.
		panic(err)
	}
	return out.String()
}

// browserActions lists every value BrowserParams.Action accepts.
var browserActions = []string{
	"navigate", "back", "forward", "click", "type", "key", "scroll", "eval",
	"text", "html", "snapshot", "images", "console", "dialog", "cdp",
	"screenshot", "url", "close", "export_test", "trace_start", "trace_stop",
	"vault_list", "vault_fill",
	"find", "assert", "semantic_click", "semantic_type", "tabs", "tab_new", "tab_select", "tab_close", "frames", "network", "capture_region", "upload", "download_start", "download_wait", "handoff", "status", "trace", "auth_code",
}

// browserReadOnlyActions are the actions that only observe the current page
// (or the manager's bookkeeping) rather than changing what's loaded or
// submitting anything -- these get Safe: true, the same way bash marks its
// read-only command allowlist, so ModeAutoAcceptEdits doesn't stop to ask
// about them while ModePlan still denies the tool outright.
var browserReadOnlyActions = map[string]bool{
	"vault_list": true,
	"scroll":     true,
	"text":       true,
	"html":       true,
	"snapshot":   true,
	"images":     true,
	"console":    true,
	"screenshot": true,
	"url":        true,
	"close":      true,
	"find":       true, "assert": true, "tabs": true, "frames": true, "network": true, "capture_region": true, "status": true, "trace": true, "handoff": true,
}

type BrowserParams struct {
	CredentialID string          `json:"credential_id,omitempty" description:"Saved vault handle for vault_fill. Never supply passwords in tool arguments."`
	Advanced     browser.Request `json:"advanced,omitempty" description:"Parameters for find/assert/semantic input/tabs/frames/network/capture_region/upload/download actions. Action is taken from the outer action field."`
	SecretEnv    string          `json:"secret_env,omitempty" description:"For auth_code: environment variable containing an authorized TOTP seed; never supply the seed itself."`
	Action       string          `json:"action" description:"Browser action: navigate/back/forward/click/type/key/scroll/eval/text/html/snapshot/images/console/dialog/cdp/screenshot/url/close, find/assert/semantic_click/semantic_type, tabs/tab_new/tab_select/tab_close/frames/network/capture_region/upload/download_start/download_wait, handoff/status/trace/trace_start/trace_stop/export_test/auth_code/vault_list/vault_fill. See tool description."`
	// URL is required for navigate.
	URL string `json:"url,omitempty" description:"Destination for the navigate action. Must start with http:// or https://."`
	// Selector is an alternative to Ref for click, type, text, and html.
	Selector string `json:"selector,omitempty" description:"CSS selector identifying the target element, for click, type, text, and html. Prefer ref when one is available from a prior snapshot -- a hand-written selector is easier to get wrong."`
	// Ref is the preferred way to target an element for click, type,
	// text, and html: an id from a prior snapshot action.
	Ref string `json:"ref,omitempty" description:"Element ref from a prior snapshot action (e.g. \"e3\"), for click, type, text, and html. Preferred over selector."`
	// Text is required for type.
	Text string `json:"text,omitempty" description:"Text to type into the element identified by ref/selector, for the type action. Replaces whatever was already in the field."`
	// Key is required for key.
	Key string `json:"key,omitempty" description:"Named key to send to the focused element, for the key action: enter, tab, escape, backspace, delete, arrowup, arrowdown, arrowleft, arrowright."`
	// Direction and Amount are for scroll.
	Direction string `json:"direction,omitempty" description:"For the scroll action: up, down, left, or right."`
	Amount    int    `json:"amount,omitempty" description:"For the scroll action: pixels to scroll. Defaults to 800."`
	// Script is required for eval.
	Script string `json:"script,omitempty" description:"JavaScript expression to evaluate in the page, for the eval action. The expression's value is returned as JSON."`
	// Full only applies to snapshot.
	Full bool `json:"full,omitempty" description:"For the snapshot action: include elements scrolled out of the current viewport too, not just what's currently visible."`
	// FullPage only applies to screenshot.
	FullPage bool `json:"full_page,omitempty" description:"For the screenshot action, capture the full scrollable page instead of just the visible viewport."`
	// Accept and PromptText are for dialog.
	Accept     bool   `json:"accept,omitempty" description:"For the dialog action: true to accept (OK/confirm), false to dismiss (Cancel)."`
	PromptText string `json:"prompt_text,omitempty" description:"For the dialog action: text to enter before accepting a prompt() dialog. Ignored otherwise, and when accept is false."`
	// CDPMethod and CDPParams are for cdp.
	CDPMethod string          `json:"cdp_method,omitempty" description:"For the cdp action: the Chrome DevTools Protocol method name, e.g. \"Network.getCookies\"."`
	CDPParams json.RawMessage `json:"cdp_params,omitempty" description:"For the cdp action: a JSON object of parameters for the method, e.g. {}. Omit for a method that takes none."`
}

type BrowserPermissionsParams struct {
	CredentialID string          `json:"credential_id,omitempty"`
	Advanced     browser.Request `json:"advanced,omitempty"`
	SecretEnv    string          `json:"secret_env,omitempty"`
	Action       string          `json:"action"`
	URL          string          `json:"url,omitempty"`
	Selector     string          `json:"selector,omitempty"`
	Ref          string          `json:"ref,omitempty"`
	Text         string          `json:"text,omitempty"`
	Key          string          `json:"key,omitempty"`
	Direction    string          `json:"direction,omitempty"`
	Amount       int             `json:"amount,omitempty"`
	Script       string          `json:"script,omitempty"`
	Full         bool            `json:"full,omitempty"`
	FullPage     bool            `json:"full_page,omitempty"`
	Accept       bool            `json:"accept,omitempty"`
	PromptText   string          `json:"prompt_text,omitempty"`
	CDPMethod    string          `json:"cdp_method,omitempty"`
	CDPParams    json.RawMessage `json:"cdp_params,omitempty"`
}

type BrowserResponseMetadata struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
}

// browserSessions is the seam NewBrowserTool depends on instead of
// *browser.Manager directly, so tests can supply a fake session without
// launching a real browser.
type browserSessions interface {
	Session(id string) (browser.Session, error)
	Close(id string)
}

func NewBrowserTool(permissions permission.Service, workingDir string, cfg config.ToolBrowser, stores ...*vault.Store) fantasy.AgentTool {
	manager := browser.GetManager(browser.Options{
		IsolateProfiles:      true,
		OverlayDisabled:      cfg.Overlay != nil && !*cfg.Overlay,
		OverlayReducedMotion: cfg.OverlayReducedMotion,
		ExecutablePath:       cfg.ExecutablePath,
		Headless:             cfg.IsHeadless(),
		UserDataDir:          cfg.GetUserDataDir(),
		UseRealProfile:       cfg.UsesRealProfile(),
		RealProfilePin:       cfg.GetRealProfilePin(),
		RemoteURL:            cfg.GetRemoteURL(),
		ActionTimeout:        cfg.GetActionTimeout(),
		IdleTimeout:          cfg.GetIdleTimeout(),
	})
	return newBrowserTool(permissions, workingDir, manager, browserDescription(cfg.UsesRealProfile()), stores...)
}

func newBrowserTool(permissions permission.Service, workingDir string, sessions browserSessions, description string, stores ...*vault.Store) fantasy.AgentTool {
	var credentialStore *vault.Store
	if len(stores) > 0 {
		credentialStore = stores[0]
	}
	return fantasy.NewAgentTool(
		BrowserToolName,
		description,
		func(ctx context.Context, params BrowserParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			action := strings.ToLower(strings.TrimSpace(params.Action))
			if !slices.Contains(browserActions, action) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("unknown action %q, must be one of: %s", params.Action, strings.Join(browserActions, ", "))), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for using the browser")
			}
			controlID := engineering.GetScope(ctx, sessionID).SessionID
			if err := interaction.Default.Load(workingDir, controlID); err != nil {
				return fantasy.NewTextErrorResponse("Cannot load interaction state: " + err.Error()), nil
			}

			p, err := permissions.Request(
				ctx,
				permission.CreatePermissionRequest{
					SessionID:   sessionID,
					Path:        workingDir,
					ToolCallID:  call.ID,
					ToolName:    BrowserToolName,
					Action:      action,
					Description: browserActionDescription(action, params),
					Params:      BrowserPermissionsParams(params),
					Safe:        browserReadOnlyActions[action],
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return NewPermissionDeniedResponse(permissions), nil
			}
			if action == "handoff" {
				interaction.Default.Pause(controlID, "Browser authentication or user intervention required")
				if err := interaction.Default.Record(workingDir, controlID, interaction.Entry{Time: time.Now(), Resource: "browser", Action: action, Status: "paused"}); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				response := fantasy.NewTextResponse("Automation paused. Complete CAPTCHA, passkey or authentication in the visible browser, then resume from /interactions.")
				response.StopTurn = true
				return response, nil
			}
			if action == "status" || action == "trace" {
				data, _ := json.Marshal(interaction.Default.ToolSnapshot(controlID, action == "trace"))
				return fantasy.NewTextResponse(string(data)), nil
			}
			if action == "trace_start" || action == "trace_stop" {
				interaction.Default.SetImageRecording(controlID, action == "trace_start")
				if err := interaction.Default.Record(workingDir, controlID, interaction.Entry{Time: time.Now(), Resource: "browser", OwnerID: sessionID, Action: action, Status: "ready"}); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse("Before/after image recording updated. Secret-entry actions are omitted from image recording."), nil
			}
			if action == "vault_list" {
				items, err := credentialStore.List(ctx)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				data, _ := json.Marshal(items)
				return fantasy.NewTextResponse(string(data)), nil
			}
			if action == "export_test" {
				return exportInteractionTest(workingDir, controlID, params.Advanced.Paths)
			}
			resource := "browser/" + sessionID
			if manager, ok := sessions.(interface{ OwnershipResource(string) string }); ok {
				resource = manager.OwnershipResource(sessionID)
			}
			release, err := interaction.Default.Acquire(ctx, controlID, resource)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			defer release()
			if manager, ok := sessions.(interface{ UsesDesktop() bool }); ok && manager.UsesDesktop() {
				releaseDesktop, err := interaction.Default.Acquire(ctx, controlID, "desktop")
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				defer releaseDesktop()
			}
			started := time.Now()
			before, after := "", ""
			status := "failed"
			defer func() {
				entry := interaction.Entry{Time: started, Resource: "browser", OwnerID: sessionID, Before: before, After: after, Action: action, Status: status, DurationMS: time.Since(started).Milliseconds()}
				if status == "succeeded" {
					entry.Target = params.Advanced.Selector
					if entry.Target == "" {
						entry.Target = params.Selector
					}
					entry.Role = params.Advanced.Role
					entry.Name = params.Advanced.Name
					entry.Condition = params.Advanced.Condition
					if action == "navigate" {
						entry.URL = safeTraceURL(params.URL)
					}
				}
				if err := interaction.Default.Record(workingDir, controlID, entry); err != nil {
					slog.Warn("Failed to persist interaction trace", "error", err)
				}
			}()

			if action == "close" {
				sessions.Close(sessionID)
				status = "succeeded"
				return fantasy.NewTextResponse("Browser session closed."), nil
			}

			var sess browser.Session
			if manager, ok := sessions.(interface {
				SessionContext(context.Context, string) (browser.Session, error)
			}); ok {
				sess, err = manager.SessionContext(ctx, sessionID)
			} else {
				sess, err = sessions.Session(sessionID)
			}
			if err != nil {
				return fantasy.NewTextErrorResponse("failed to start browser: " + err.Error()), nil
			}
			if driver, ok := sess.(interface{ BindContext(context.Context) func() }); ok {
				defer driver.BindContext(ctx)()
			}
			if visual, ok := sess.(interface {
				BeginActivity(context.Context, string, string, string) func()
			}); ok {
				selector := params.Selector
				if params.Ref != "" {
					if resolved, err := resolveTargetSelector(action, params); err == nil {
						selector = resolved
					}
				}
				if selector == "" {
					selector = params.Advanced.Selector
				}
				defer interaction.Default.StartActivity(controlID, func() func() {
					return visual.BeginActivity(ctx, controlID, action, selector)
				})()
			}
			recordImages := !vault.Sensitive("") && interaction.Default.Snapshot(controlID).RecordImages && recordableBrowserAction(action)
			if recordImages {
				before = captureBrowserTrace(ctx, sess, workingDir, controlID)
			}

			var response fantasy.ToolResponse
			if action == "vault_fill" {
				response, err = fillBrowserVault(ctx, sess, credentialStore, params)
			} else if action == "auth_code" {
				response, err = runBrowserAuth(ctx, sess, params)
			} else if isAdvancedBrowserAction(action) {
				response, err = runAdvancedBrowser(ctx, sess, action, params, workingDir)
			} else {
				response, err = runBrowserAction(sess, action, params)
			}
			if err == nil && !response.IsError {
				status = "succeeded"
			}
			if recordImages {
				after = captureBrowserTrace(ctx, sess, workingDir, controlID)
			}
			return withInteractionFailure(response), err
		},
	)
}

// describeTarget renders whichever of ref/selector a call used, for
// permission prompts and confirmation messages.
func describeTarget(params BrowserParams) string {
	if params.Ref != "" {
		return "ref " + params.Ref
	}
	return params.Selector
}

func browserActionDescription(action string, params BrowserParams) string {
	switch action {
	case "navigate":
		return "Navigate browser to: " + params.URL
	case "back":
		return "Navigate browser back"
	case "forward":
		return "Navigate browser forward"
	case "click":
		return "Click browser element: " + describeTarget(params)
	case "type":
		return "Type into browser element: " + describeTarget(params)
	case "key":
		return "Send browser key press: " + params.Key
	case "scroll":
		return "Scroll browser: " + params.Direction
	case "eval":
		return "Run JavaScript in browser: " + params.Script
	case "text", "html":
		return fmt.Sprintf("Read browser element %s: %s", action, describeTarget(params))
	case "snapshot":
		return "Snapshot interactive elements on the page"
	case "images":
		return "List images on the page"
	case "console":
		return "Read browser console output"
	case "dialog":
		verb := "Dismiss"
		if params.Accept {
			verb = "Accept"
		}
		return verb + " the pending browser dialog"
	case "cdp":
		return "Send raw CDP command: " + params.CDPMethod
	case "screenshot":
		return "Take browser screenshot"
	case "url":
		return "Read current browser URL"
	case "close":
		return "Close browser session"
	default:
		return "Browser action: " + action
	}
}

// resolveTargetSelector builds the CSS selector click/type/text/html
// should act on: a ref from a prior snapshot when one is given
// (preferred -- see the tool description for why), otherwise a
// caller-supplied CSS selector.
func resolveTargetSelector(action string, params BrowserParams) (string, error) {
	if params.Ref != "" {
		if strings.ContainsAny(params.Ref, `"[]`) {
			return "", fmt.Errorf("invalid ref %q -- use a ref exactly as shown by a prior snapshot action", params.Ref)
		}
		return fmt.Sprintf(`[data-atlas-ref="%s"]`, params.Ref), nil
	}
	if params.Selector != "" {
		return params.Selector, nil
	}
	return "", fmt.Errorf("ref or selector is required for the %s action", action)
}

// clickFallback handles a failed selector click. With a ref, it takes
// a fresh snapshot and presses the element's center instead of failing
// outright: the DOM often moved between the model's snapshot and its
// click. Without a ref there is no box to aim at, so the backend error
// (which already hints at staleness) stands as-is.
func clickFallback(sess browser.Session, metadata BrowserResponseMetadata, params BrowserParams, selector string, clickErr error) (fantasy.ToolResponse, error) {
	code, _ := interaction.Failure(clickErr.Error())
	if code == "timeout" || code == "ambiguous_target" || code == "target_not_actionable" {
		return fantasy.NewTextErrorResponse("click failed: " + clickErr.Error() + "; observe and verify before retrying input"), nil
	}
	if params.Ref == "" {
		return fantasy.NewTextErrorResponse("click failed: " + clickErr.Error()), nil
	}
	elements, err := sess.Snapshot(false)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"click failed: %s (fresh snapshot also failed: %s).", clickErr.Error(), err.Error(),
		)), nil
	}
	for _, el := range elements {
		if el.Ref != params.Ref {
			continue
		}
		x, y := el.Rect.Center()
		if err := sess.ClickAt(x, y); err != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf(
				"click failed: %s (coordinate fallback at (%.0f, %.0f) also failed: %s).",
				clickErr.Error(), x, y, err.Error(),
			)), nil
		}
		return withFreshState(sess, metadata, fmt.Sprintf(
			"Clicked %s at (%.0f, %.0f) via coordinate fallback.",
			describeTarget(params), x, y,
		))
	}
	return fantasy.NewTextErrorResponse(fmt.Sprintf(
		"click failed: %s (ref %q is gone from the page; take a new snapshot and re-aim).",
		clickErr.Error(), params.Ref,
	)), nil
}

// defaultScrollAmount is how far, in pixels, a scroll action moves when
// the caller does not specify amount.
const defaultScrollAmount = 800

// scrollDelta converts a named direction into the (dx, dy) pixel offset
// Session.Scroll expects.
func scrollDelta(direction string, amount int) (dx, dy int, err error) {
	if amount <= 0 {
		amount = defaultScrollAmount
	}
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up":
		return 0, -amount, nil
	case "down":
		return 0, amount, nil
	case "left":
		return -amount, 0, nil
	case "right":
		return amount, 0, nil
	default:
		return 0, 0, fmt.Errorf("unsupported direction %q, must be one of: up, down, left, right", direction)
	}
}

// freshSnapshotCap bounds the snapshot attached to state-changing
// action responses. Enough to act on without another round trip, too
// small to flood context on element-dense pages.
const freshSnapshotCap = 100

// withFreshState appends the page's current interactive elements to a
// successful state-changing action's response. Refs die on navigation
// and many clicks mutate the DOM, so handing the model a fresh list
// with the result removes the whole class of stale-ref retries.
func withFreshState(sess browser.Session, metadata BrowserResponseMetadata, msg string) (fantasy.ToolResponse, error) {
	var b strings.Builder
	b.WriteString(msg)
	elements, err := sess.Snapshot(false)
	if err != nil {
		fmt.Fprintf(&b, "\n\nFresh snapshot unavailable: %s.", err.Error())
	} else {
		if len(elements) > freshSnapshotCap {
			elements = elements[:freshSnapshotCap]
		}
		b.WriteString("\n\nInteractive elements now:\n")
		b.WriteString(formatSnapshot(elements, sess.PendingDialogs()))
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), metadata), nil
}

func runBrowserAction(sess browser.Session, action string, params BrowserParams) (fantasy.ToolResponse, error) {
	metadata := BrowserResponseMetadata{Action: action}

	switch action {
	case "navigate":
		if params.URL == "" {
			return fantasy.NewTextErrorResponse("url is required for the navigate action"), nil
		}
		if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
			return fantasy.NewTextErrorResponse("url must start with http:// or https://"), nil
		}
		if err := sess.Navigate(params.URL); err != nil {
			return fantasy.NewTextErrorResponse("navigate failed: " + err.Error()), nil
		}
		metadata.URL = params.URL
		return withFreshState(sess, metadata, "Navigated to "+params.URL)

	case "back":
		if err := sess.Back(); err != nil {
			return fantasy.NewTextErrorResponse("back failed: " + err.Error()), nil
		}
		return withFreshState(sess, metadata, "Navigated back.")

	case "forward":
		if err := sess.Forward(); err != nil {
			return fantasy.NewTextErrorResponse("forward failed: " + err.Error()), nil
		}
		return withFreshState(sess, metadata, "Navigated forward.")

	case "click":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Click(selector); err != nil {
			return clickFallback(sess, metadata, params, selector, err)
		}
		return withFreshState(sess, metadata, "Clicked "+describeTarget(params))

	case "type":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Type(selector, params.Text); err != nil {
			return fantasy.NewTextErrorResponse("type failed: " + err.Error()), nil
		}
		return withFreshState(sess, metadata, "Typed into "+describeTarget(params))

	case "key":
		if params.Key == "" {
			return fantasy.NewTextErrorResponse("key is required for the key action"), nil
		}
		if err := sess.PressKey(strings.ToLower(params.Key)); err != nil {
			return fantasy.NewTextErrorResponse("key press failed: " + err.Error()), nil
		}
		return withFreshState(sess, metadata, "Sent key "+params.Key)

	case "scroll":
		dx, dy, err := scrollDelta(params.Direction, params.Amount)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Scroll(dx, dy); err != nil {
			return fantasy.NewTextErrorResponse("scroll failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Scrolled "+strings.ToLower(params.Direction)+"."), metadata), nil

	case "eval":
		if params.Script == "" {
			return fantasy.NewTextErrorResponse("script is required for the eval action"), nil
		}
		result, err := sess.Eval(params.Script)
		if err != nil {
			return fantasy.NewTextErrorResponse("eval failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil

	case "text":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		result, err := sess.Text(selector)
		if err != nil {
			return fantasy.NewTextErrorResponse("text failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil

	case "html":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		result, err := sess.HTML(selector)
		if err != nil {
			return fantasy.NewTextErrorResponse("html failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil

	case "snapshot":
		elements, err := sess.Snapshot(params.Full)
		if err != nil {
			return fantasy.NewTextErrorResponse("snapshot failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(formatSnapshot(elements, sess.PendingDialogs())), metadata), nil

	case "images":
		images, err := sess.Images()
		if err != nil {
			return fantasy.NewTextErrorResponse("images failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(formatImages(images)), metadata), nil

	case "console":
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(formatConsoleLogs(sess.ConsoleLogs())), metadata), nil

	case "dialog":
		if err := sess.HandleDialog(params.Accept, params.PromptText); err != nil {
			return fantasy.NewTextErrorResponse("dialog failed: " + err.Error()), nil
		}
		verb := "Dismissed"
		if params.Accept {
			verb = "Accepted"
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(verb+" the pending dialog."), metadata), nil

	case "cdp":
		if params.CDPMethod == "" {
			return fantasy.NewTextErrorResponse("cdp_method is required for the cdp action"), nil
		}
		var cdpParams map[string]any
		if len(params.CDPParams) > 0 {
			if err := json.Unmarshal(params.CDPParams, &cdpParams); err != nil {
				return fantasy.NewTextErrorResponse("cdp_params must be a JSON object: " + err.Error()), nil
			}
		}
		result, err := sess.RawCDP(params.CDPMethod, cdpParams)
		if err != nil {
			return fantasy.NewTextErrorResponse("cdp command failed: " + err.Error()), nil
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return fantasy.NewTextErrorResponse("failed to encode cdp result: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(string(resultJSON)), metadata), nil

	case "screenshot":
		data, err := sess.AnnotatedScreenshot(params.FullPage)
		if err != nil {
			return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
		}
		resp := fantasy.NewImageResponse(data, "image/png")
		resp.Content = "Numbered boxes mark interactive elements, labeled with their snapshot ref. " +
			"Act with click/type using the shown ref, not pixel coordinates."
		return resp, nil

	case "url":
		result, err := sess.URL()
		if err != nil {
			return fantasy.NewTextErrorResponse("url failed: " + err.Error()), nil
		}
		metadata.URL = result
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil

	default:
		return fantasy.NewTextErrorResponse("unknown action: " + action), nil
	}
}

// formatSnapshot renders a page snapshot as one line per element --
// [ref] role "name" value="..." (tag) -- with any dialogs blocking the
// page called out first, since nothing else will succeed until one is
// answered.
func formatSnapshot(elements []browser.SnapshotElement, dialogs []browser.DialogInfo) string {
	var b strings.Builder
	if len(dialogs) > 0 {
		fmt.Fprintf(&b, "%d pending dialog(s) block the page -- resolve with the dialog action first:\n", len(dialogs))
		for _, d := range dialogs {
			fmt.Fprintf(&b, "- %s: %q\n", d.Type, d.Message)
		}
		b.WriteString("\n")
	}
	if len(elements) == 0 {
		b.WriteString("No interactive elements found.")
		return b.String()
	}
	for _, el := range elements {
		fmt.Fprintf(&b, "[%s] %s", el.Ref, el.Role)
		if el.Name != "" {
			fmt.Fprintf(&b, " %q", el.Name)
		}
		if el.Value != "" {
			fmt.Fprintf(&b, " value=%q", el.Value)
		}
		fmt.Fprintf(&b, " (%s)\n", el.Tag)
	}
	return b.String()
}

func formatImages(images []browser.ImageInfo) string {
	if len(images) == 0 {
		return "No images found."
	}
	var b strings.Builder
	for _, img := range images {
		b.WriteString(img.Src)
		if img.Alt != "" {
			fmt.Fprintf(&b, " -- %q", img.Alt)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatConsoleLogs(entries []browser.ConsoleEntry) string {
	if len(entries) == 0 {
		return "No console output captured."
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] %s: %s\n", e.Time.Format("15:04:05"), e.Type, e.Text)
	}
	return b.String()
}
