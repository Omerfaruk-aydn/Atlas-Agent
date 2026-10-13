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

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
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
	"wait_for", "popup_wait", "dialog_wait", "dialog_handle",
	"navigate", "back", "forward", "click", "type", "key", "scroll", "eval",
	"text", "html", "snapshot", "images", "console", "dialog", "cdp",
	"screenshot", "url", "close", "export_test", "trace_start", "trace_stop",
	"vault_list", "vault_fill", "credential_request", "login_check",
	"find", "assert", "semantic_click", "semantic_type", "tabs", "tab_new", "tab_select", "tab_close", "frames", "network", "capture_region", "upload", "download_start", "download_wait", "handoff", "status", "trace", "auth_code",
	browser.InputPointerMove, browser.InputPointerClick, browser.InputPointerDrag, browser.InputCSSDrag, browser.InputKeyCombo, browser.InputInsertText,
	"observe", "set_field", "read_field", "select_option", "fill_form", "submit", "collect", "research", "health", "tab_return", "undo", "knowledge",
	"task_plan", "task_update", "task_status",
	"layout_audit",
	"captcha_detect", "captcha_solve", "captcha_fill",
}

// browserReadOnlyActions are the actions that only observe the current page
// (or the manager's bookkeeping) rather than changing what's loaded or
// submitting anything -- these get Safe: true, the same way bash marks its
// read-only command allowlist, so ModeAutoAcceptEdits doesn't stop to ask
// about them while ModePlan still denies the tool outright.
var browserReadOnlyActions = map[string]bool{
	"wait_for": true, "popup_wait": true, "dialog_wait": true,
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
	"observe": true, "read_field": true, "health": true, "knowledge": true, "task_plan": true, "task_update": true, "task_status": true,
	"layout_audit": true, "login_check": true,
	"captcha_detect": true,
}

type BrowserParams struct {
	CredentialID string          `json:"credential_id,omitempty" description:"For vault_fill: a saved vault handle or a temporary handle (cr_...) returned by credential_request. Never supply passwords in tool arguments."`
	Service      string          `json:"service,omitempty" description:"For credential_request: the service the user signs in to, shown on their phone (for example \"Google\"). Not a secret."`
	Account      string          `json:"account,omitempty" description:"For credential_request/vault_fill: the account identifier (email or username) the password belongs to, when known. Never a password."`
	Advanced     browser.Request `json:"advanced,omitempty" description:"Parameters for semantic targets/scope, document/tab/origin guards, conditions, tabs/popups/dialogs, frames, upload/download. Action is taken from the outer action field."`
	SecretEnv    string          `json:"secret_env,omitempty" description:"For auth_code: environment variable containing an authorized TOTP seed; never supply the seed itself."`
	Action       string          `json:"action" description:"Browser action: navigate/back/forward/click/type/key/scroll/eval/text/html/snapshot/images/console/dialog/cdp/screenshot/url/close, find/assert/wait_for/semantic_click/semantic_type, tabs/tab_new/tab_select/tab_close/popup_wait/dialog_wait/dialog_handle/frames/network/capture_region/upload/download_start/download_wait, handoff/status/trace/trace_start/trace_stop/export_test/auth_code/vault_list/vault_fill, captcha_detect/captcha_solve/captcha_fill, pointer_move/pointer_click/pointer_drag/css_drag/key_combo/insert_text. See tool description."`
	// URL is required for navigate.
	URL string `json:"url,omitempty" description:"Destination for the navigate action. Must start with http:// or https://."`
	// Selector is an alternative to Ref for click, type, text, and html.
	Selector string `json:"selector,omitempty" description:"CSS selector identifying the target element, for click, type, text, and html. Prefer ref when one is available from a prior snapshot -- a hand-written selector is easier to get wrong."`
	// Ref is the preferred way to target an element for click, type,
	// text, and html: an id from a prior snapshot action.
	Ref string `json:"ref,omitempty" description:"Element ref from a prior snapshot action (e.g. \"e3\"), for click, type, text, and html. Preferred over selector."`
	// Value advertises the top-level alias of advanced.value. UnmarshalJSON
	// moves it into Advanced and keeps this field empty, so it only exists
	// for the schema: the model is offered exactly what the runtime accepts.
	Value string `json:"value,omitempty" description:"For set_field/select_option: the value to apply, same as advanced.value (send one of them). Required; an empty string clears the field."`
	// Text is required for type.
	Text string `json:"text,omitempty" description:"For type: text that replaces the field identified by ref/selector. For insert_text: text typed at the focused element. For handoff: the exact step only the user can do (no secrets)."`
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
	// Annotate defaults to true; false captures clean pixels for delivery.
	Annotate *bool `json:"annotate,omitempty" description:"For screenshot: false returns a clean image without target boxes, for visual validation or delivery. Defaults to true. Clean captures do not issue pointer viewport grants."`
	// Accept and PromptText are for dialog.
	Accept     bool   `json:"accept,omitempty" description:"For the dialog action: true to accept (OK/confirm), false to dismiss (Cancel)."`
	PromptText string `json:"prompt_text,omitempty" description:"For the dialog action: text to enter before accepting a prompt() dialog. Ignored otherwise, and when accept is false."`
	// CDPMethod and CDPParams are for cdp.
	CDPMethod string           `json:"cdp_method,omitempty" description:"For the cdp action: the Chrome DevTools Protocol method name, e.g. \"Network.getCookies\"."`
	CDPParams BrowserCDPParams `json:"cdp_params,omitempty" description:"For the cdp action: a JSON object (not a quoted JSON string) of parameters for the method, e.g. {\"urls\":[\"https://example.com\"]}. Omit for a method that takes none."`
	// Task carries completion-contract updates for task_plan/task_update.
	Task *BrowserTaskParams `json:"task,omitempty" description:"For task_plan/task_update: the completion contract of a multi-part browser deliverable."`
}

type BrowserPermissionsParams struct {
	CredentialID string             `json:"credential_id,omitempty"`
	Service      string             `json:"service,omitempty"`
	Account      string             `json:"account,omitempty"`
	Advanced     browser.Request    `json:"advanced,omitempty"`
	SecretEnv    string             `json:"secret_env,omitempty"`
	Action       string             `json:"action"`
	URL          string             `json:"url,omitempty"`
	Selector     string             `json:"selector,omitempty"`
	Ref          string             `json:"ref,omitempty"`
	Value        string             `json:"value,omitempty"`
	Text         string             `json:"text,omitempty"`
	Key          string             `json:"key,omitempty"`
	Direction    string             `json:"direction,omitempty"`
	Amount       int                `json:"amount,omitempty"`
	Script       string             `json:"script,omitempty"`
	Full         bool               `json:"full,omitempty"`
	FullPage     bool               `json:"full_page,omitempty"`
	Annotate     *bool              `json:"annotate,omitempty"`
	Accept       bool               `json:"accept,omitempty"`
	PromptText   string             `json:"prompt_text,omitempty"`
	CDPMethod    string             `json:"cdp_method,omitempty"`
	CDPParams    BrowserCDPParams   `json:"cdp_params,omitempty"`
	Task         *BrowserTaskParams `json:"task,omitempty"`
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
			if err := browserArgumentPreflight(action, params, false); err != nil {
				return errorResponse(err), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for using the browser")
			}
			stateID := sessionID
			if _, shared := sessions.(*browser.Manager); !shared {
				stateID = fmt.Sprintf("%s@%p", sessionID, sessions)
			}
			ctx = withBrowserState(ctx, stateID)
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
					Safe:        browserReadOnlyActions[action] || credentialAuthorized(action, params, sessionID),
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return NewPermissionDeniedResponse(permissions), nil
			}
			if action == "handoff" {
				step, err := handoffStep(params.Text)
				if err != nil {
					return contractResponse(err), nil
				}
				if redirect := handoffRedirect(step); redirect != nil {
					return contractResponse(redirect), nil
				}
				interaction.Default.Pause(controlID, step)
				if err := interaction.Default.Record(workingDir, controlID, interaction.Entry{Time: time.Now(), Resource: "browser", Action: action, Status: "paused"}); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return handoffResponse(step, "in the visible browser"), nil
			}
			if action == "status" || action == "trace" {
				data, _ := json.Marshal(interaction.Default.ToolSnapshot(controlID, action == "trace"))
				if action == "status" {
					var state map[string]any
					if json.Unmarshal(data, &state) == nil {
						state["browser_metrics"] = BrowserSessionMetrics(stateID)
						data, _ = json.Marshal(state)
					}
				}
				return fantasy.NewTextResponse(string(data)), nil
			}
			if browserTaskActions[action] {
				task := BrowserTaskParams{}
				if params.Task != nil {
					task = *params.Task
				}
				out, err := runBrowserTask(workingDir, sessionID, stateID, action, task)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(out), nil
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
			if action == "credential_request" {
				sess, err := browserSessionFor(ctx, sessions, sessionID)
				if err != nil {
					return fantasy.NewTextErrorResponse("failed to start browser: " + err.Error()), nil
				}
				return runCredentialRequest(ctx, sess, credentialStore, params, sessionID, call.ID, controlID, resource)
			}
			release, err := interaction.Default.Acquire(ctx, controlID, resource)
			if err != nil {
				return interactionAcquireResponse(err), nil
			}
			defer release()
			if manager, ok := sessions.(interface{ UsesDesktop() bool }); ok && manager.UsesDesktop() {
				releaseDesktop, err := interaction.Default.Acquire(ctx, controlID, "desktop")
				if err != nil {
					return interactionAcquireResponse(err), nil
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
				browserObservations.invalidate(stateID)
				status = "succeeded"
				return fantasy.NewTextResponse("Browser session closed."), nil
			}

			var sess browser.Session
			bootstrap, canBootstrap := sessions.(interface {
				SessionContextURL(context.Context, string, string) (browser.Session, error)
			})
			unguarded := params.Advanced.ExpectedTabID == "" && params.Advanced.ExpectedOrigin == "" && params.Advanced.DocumentID == ""
			if canBootstrap && action == "navigate" && unguarded && !interaction.Default.Snapshot(controlID).RecordImages {
				sess, err = bootstrap.SessionContextURL(ctx, sessionID, params.URL)
			} else if manager, ok := sessions.(interface {
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
			if params.Advanced.ExpectedTabID != "" || params.Advanced.ExpectedOrigin != "" || params.Advanced.DocumentID != "" {
				guard, ok := sess.(interface {
					CheckDocument(context.Context, browser.Request) error
				})
				if !ok {
					return fantasy.NewTextErrorResponse("Browser driver cannot enforce the requested tab/document/origin guard"), nil
				}
				if err := guard.CheckDocument(ctx, params.Advanced); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
			}
			var operation activity.Operation
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
					if outcome, ok := visual.(interface {
						StartActivity(context.Context, string, string, string) activity.Operation
					}); ok {
						operation = outcome.StartActivity(ctx, controlID, action, selector)
						return operation.End
					}
					return visual.BeginActivity(ctx, controlID, action, selector)
				})()
			}
			recordImages := !vault.Sensitive("") && !credentialSourceRead(ctx, action) && interaction.Default.Snapshot(controlID).RecordImages && recordableBrowserAction(action)
			if recordImages {
				before = captureBrowserTrace(ctx, sess, workingDir, controlID)
			}

			var response fantasy.ToolResponse
			if guardErr := ledgerFor(stateID).guardRepeat(stateID, action, params); guardErr != nil {
				response = fantasy.NewTextErrorResponse(guardErr.Error())
			} else if refErr := staleRefGuard(ctx, sess, stateID, action, params); refErr != nil {
				response = fantasy.NewTextErrorResponse(refErr.Error())
			} else if typeErr := keyWouldType(ctx, sess, action, params); typeErr != nil {
				response = errorResponse(typeErr)
			} else if action == "vault_fill" && vault.IsCredentialHandle(params.CredentialID) {
				response, err = fillBrowserCredential(ctx, sess, credentialStore, params, sessionID)
			} else if action == "vault_fill" {
				response, err = fillBrowserVault(ctx, sess, credentialStore, params)
			} else if action == "login_check" {
				response, err = runLoginCheck(ctx, sess, params)
			} else if action == "auth_code" {
				response, err = runBrowserAuth(ctx, sess, params)
			} else if captchaActions[action] {
				response, err = runBrowserCaptcha(ctx, sess, action, params, sessionID)
			} else if browser.IsInputAction(action) {
				response, err = runBrowserInput(ctx, sess, stateID, action, params)
			} else if browserExtendedActions[action] {
				response, err = runExtendedBrowser(ctx, sess, action, params, workingDir, stateID)
			} else if isAdvancedBrowserAction(action) {
				response, err = runAdvancedBrowser(ctx, sess, action, params, workingDir)
			} else {
				response, err = runBrowserAction(ctx, sess, action, params)
			}
			if err == nil && response.IsError {
				response.Content = dialogBlockAppends(sess, response.Content)
			}
			if err == nil && response.IsError && strings.Contains(response.Content, "password_field") {
				// The refused value may be a password the model copied from
				// chat. Protect it from every later observation and request.
				for _, value := range refusedSecretCandidates(params) {
					_ = vault.Register(sessionID, value)
				}
				response = passwordFieldGuidance(ctx, sess, credentialStore, response)
			}
			if err == nil && !response.IsError {
				status = "succeeded"
			} else {
				// Only a verified failure earns the banner's blocked reaction.
				operation.Fail()
			}
			if recordImages {
				after = captureBrowserTrace(ctx, sess, workingDir, controlID)
			}
			if err == nil {
				var exec browser.ExecutionIdentity
				if identity, ok := sess.(browser.IdentitySession); ok {
					exec = identity.ExecutionIdentity(ctx)
				} else if tabs, ok := sess.(browser.TabIdentity); ok {
					exec.TabID = tabs.ActiveTab()
				}
				if action == "find" && !response.IsError {
					var found struct {
						Matches []struct {
							Ref string `json:"ref"`
						} `json:"matches"`
					}
					if json.Unmarshal([]byte(response.Content), &found) == nil {
						var refs []string
						for _, match := range found.Matches {
							refs = append(refs, match.Ref)
						}
						browserObservations.grantFoundRefs(ctx, stateID, sess, exec, refs)
					}
				}
				if !response.IsError {
					trackCanvasTextEdit(ctx, sess, stateID, action, params, exec)
				}
				if params.Ref != "" && !response.IsError && (action == "set_field" || action == "select_option" || action == "read_field" || action == "type") {
					// The result reports the field as it is now; a field whose
					// name carries its value keeps a usable ref.
					browserObservations.grantFoundRefs(ctx, stateID, sess, exec, []string{params.Ref})
				}
				var op BrowserOp
				response, op = recordBrowserOp(stateID, action, params, response, started, exec)
				if !credentialOwnedOutcome(action, params) {
					response = withToolOutcome(response, browserOutcome(op))
				}
				recordBrowserStep(workingDir, sessionID, op)
			}
			return withBrowserFailure(response), err
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
	case browser.InputPointerMove, browser.InputPointerClick:
		return fmt.Sprintf("Browser %s at (%.0f, %.0f)", strings.ReplaceAll(action, "_", " "), params.Advanced.X, params.Advanced.Y)
	case browser.InputPointerDrag:
		return fmt.Sprintf("Browser pointer drag from (%.0f, %.0f) to (%.0f, %.0f)", params.Advanced.X, params.Advanced.Y, params.Advanced.ToX, params.Advanced.ToY)
	case browser.InputKeyCombo:
		return "Send browser key combination: " + strings.Join(append(append([]string(nil), params.Advanced.Modifiers...), firstNonEmpty(params.Advanced.Key, params.Key)), "+")
	case browser.InputInsertText:
		return "Insert text at the focused browser element"
	case "observe":
		return "Observe the page (" + firstNonEmpty(params.Advanced.Mode, "delta") + ")"
	case "set_field":
		return "Set and commit browser field: " + firstNonEmpty(describeTarget(params), params.Advanced.Label, params.Advanced.Name, params.Advanced.Selector)
	case "read_field":
		return "Read browser field: " + firstNonEmpty(describeTarget(params), params.Advanced.Label, params.Advanced.Name, params.Advanced.Selector)
	case "select_option":
		return "Select option " + params.Advanced.Value + " in: " + firstNonEmpty(describeTarget(params), params.Advanced.Label, params.Advanced.Name, params.Advanced.Selector)
	case "credential_request":
		return "Open a secure credential card on the paired phone for " + firstNonEmpty(params.Service, "the visible password field")
	case "login_check":
		return "Observe the sign-in state of the page"
	case "fill_form":
		return fmt.Sprintf("Fill %d browser form field(s)", len(params.Advanced.Fields))
	case "submit":
		return "Submit browser form via: " + firstNonEmpty(describeTarget(params), params.Advanced.Name, params.Advanced.Selector)
	case "collect":
		return "Collect list rows: " + params.Advanced.RowSelector
	case "research":
		return fmt.Sprintf("Read %d page(s) in background tabs", max(len(params.Advanced.URLs), 1))
	case "undo":
		return "Undo the last browser field edit made by Atlas"
	case "screenshot":
		return "Take browser screenshot"
	case "url":
		return "Read current browser URL"
	case "captcha_detect":
		return "Detect any CAPTCHA/verification widget on the page"
	case "captcha_solve":
		return "Solve the visible CAPTCHA or return its challenge image for you to read"
	case "captcha_fill":
		return "Type the CAPTCHA answer into its answer field"
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
func clickFallback(ctx context.Context, sess browser.Session, metadata BrowserResponseMetadata, params BrowserParams, selector string, clickErr error) (fantasy.ToolResponse, error) {
	// Only an unexplained driver failure may fall back to the element's
	// coordinates. A covered, absent, moved or ambiguous target is the
	// reason not to click there: the point would hit something else.
	code, _ := browserErrorCode(clickErr.Error())
	if code != "driver_error" || strings.Contains(clickErr.Error(), "target_changed_before_dispatch") {
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
		return withFreshState(ctx, sess, metadata, fmt.Sprintf(
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

// withFreshState appends what changed on the page to a successful
// state-changing action's response. The first observation of a document is
// a full list; later ones carry only added, removed and changed elements,
// with an observation_id the model can relate to the previous one. Refs die
// on navigation and many clicks mutate the DOM, so handing the model the
// change with the result removes the whole class of stale-ref retries
// without resending an unchanged page after every key.
func withFreshState(ctx context.Context, sess browser.Session, metadata BrowserResponseMetadata, msg string) (fantasy.ToolResponse, error) {
	var b strings.Builder
	b.WriteString(msg)
	obs, err := takeObservation(ctx, sess, browserStateID(ctx), observeRequest{mode: "delta", limit: 100})
	if err != nil {
		fmt.Fprintf(&b, "\n\nFresh snapshot unavailable: %s.", err.Error())
	} else if obs.mode == "delta" {
		b.WriteString("\n\nChanges since " + obs.baseID + ":\n")
		b.WriteString(obs.text)
	} else {
		b.WriteString("\n\nInteractive elements now:\n")
		b.WriteString(obs.text)
	}
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), metadata), nil
}

// dialogBlockAppends surfaces a pending native dialog in a failed
// action's error. A beforeunload/confirm dialog silently blocks every
// subsequent navigation and click, so the model must see it immediately
// rather than discovering it on the next observe.
func dialogBlockAppends(sess browser.Session, msg string) string {
	dialogs := sess.PendingDialogs()
	if len(dialogs) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	fmt.Fprintf(&b, "\n\n>>> %d pending dialog(s) block the page. Resolve with {\"action\":\"dialog\",\"accept\":true} (or dialog_handle) before any other action.", len(dialogs))
	for i, d := range dialogs {
		if i >= 3 {
			break
		}
		if d.Message != "" {
			fmt.Fprintf(&b, "\n  [%s] %q", d.Type, d.Message)
		} else {
			fmt.Fprintf(&b, "\n  [%s]", d.Type)
		}
	}
	return b.String()
}

func runBrowserAction(ctx context.Context, sess browser.Session, action string, params BrowserParams) (fantasy.ToolResponse, error) {
	metadata := BrowserResponseMetadata{Action: action}

	switch action {
	case "layout_audit":
		if params.Script != "" {
			return fantasy.NewTextErrorResponse("layout_audit accepts no arbitrary script"), nil
		}
		return guardedEval(ctx, sess, BrowserParams{Script: designAuditScript})
	case "navigate":
		if params.URL == "" {
			return fantasy.NewTextErrorResponse("url is required for the navigate action"), nil
		}
		if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
			return fantasy.NewTextErrorResponse("url must start with http:// or https://"), nil
		}
		if err := sess.Navigate(params.URL); err != nil {
			return fantasy.NewTextErrorResponse(dialogBlockAppends(sess, "navigate failed: "+err.Error())), nil
		}
		metadata.URL = params.URL
		return withFreshState(ctx, sess, metadata, "Navigated to "+params.URL)

	case "back":
		if err := sess.Back(); err != nil {
			return fantasy.NewTextErrorResponse(dialogBlockAppends(sess, "back failed: "+err.Error())), nil
		}
		return withFreshState(ctx, sess, metadata, "Navigated back.")

	case "forward":
		if err := sess.Forward(); err != nil {
			return fantasy.NewTextErrorResponse(dialogBlockAppends(sess, "forward failed: "+err.Error())), nil
		}
		return withFreshState(ctx, sess, metadata, "Navigated forward.")

	case "click":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Click(selector); err != nil {
			return clickFallback(ctx, sess, metadata, params, selector, err)
		}
		return withFreshState(ctx, sess, metadata, "Clicked "+describeTarget(params))

	case "type":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Type(selector, params.Text); err != nil {
			return fantasy.NewTextErrorResponse(dialogBlockAppends(sess, "type failed: "+err.Error())), nil
		}
		return withFreshState(ctx, sess, metadata, "Typed into "+describeTarget(params))

	case "key":
		if params.Key == "" {
			return fantasy.NewTextErrorResponse("key is required for the key action"), nil
		}
		if err := pressBrowserKey(ctx, sess, params.Key); err != nil {
			if ctx.Err() != nil {
				return fantasy.ToolResponse{}, ctx.Err()
			}
			return fantasy.NewTextErrorResponse("key press failed: " + err.Error()), nil
		}
		return withFreshState(ctx, sess, metadata, "Sent key "+params.Key)

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
		return guardedEval(ctx, sess, params)

	case "text":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		result, err := sess.Text(selector)
		if err != nil {
			return fantasy.NewTextErrorResponse("text failed: " + err.Error()), nil
		}
		currentURL, _ := sess.URL()
		result = importSourceCredential(ctx, currentURL, result)
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
		obs, err := takeObservation(ctx, sess, browserStateID(ctx), observeRequest{mode: "full", full: params.Full, cursor: params.Advanced.Cursor})
		if err != nil {
			return fantasy.NewTextErrorResponse("snapshot failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(obs.text), metadata), nil

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
		cdpParams, err := decodeCDPParams(params.CDPParams)
		if err != nil {
			return contractResponse(computer.NewContractError("invalid_cdp_params", "cdp_params", err.Error()+". No CDP command sent", "Resend the same method with cdp_params as a JSON object value.", `{"action":"cdp","cdp_method":"Input.dispatchMouseEvent","cdp_params":{"type":"mouseMoved","x":120,"y":80}}`)), nil
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
		if vault.Sensitive("") {
			redact := func(text string) string { return vault.Redact(GetSessionFromContext(ctx), text) }
			if viewport, ok := sess.(browser.ProtectedViewportSession); ok && !params.FullPage && (params.Annotate == nil || *params.Annotate) {
				data, vp, masks, err := viewport.ProtectedViewportScreenshot(ctx, redact)
				if err != nil {
					return fantasy.NewTextErrorResponse("Protected screenshot failed: " + err.Error()), nil
				}
				grantScreenshotRefs(ctx, browserStateID(ctx), sess)
				response := viewportImageResponse(data, vp)
				response.Content += " Editable fields, known plaintext credentials and opaque media are masked."
				return withCredentialImageProof(response, masks), nil
			}
			protected, ok := sess.(browser.ProtectedScreenshotSession)
			if !ok {
				return fantasy.NewTextErrorResponse("credential_redaction_unavailable: this driver cannot produce a protected screenshot"), nil
			}
			annotated := params.Annotate == nil || *params.Annotate
			data, masks, err := protected.ProtectedScreenshot(ctx, params.FullPage, annotated, redact)
			if err != nil {
				return fantasy.NewTextErrorResponse("Protected screenshot failed: " + err.Error()), nil
			}
			if annotated {
				grantScreenshotRefs(ctx, browserStateID(ctx), sess)
			}
			resp := fantasy.NewImageResponse(data, "image/png")
			resp.Content = "Numbered boxes mark interactive elements. Protected screenshot: editable fields, known plaintext credentials and opaque media are masked. Use observed refs for input; no new pixel-coordinate viewport grant was issued."
			if !annotated {
				resp.Content = "Clean protected screenshot without target annotations. No pointer viewport grant was issued; use current semantic refs. Editable fields, known plaintext credentials and opaque media are masked."
			}
			return withCredentialImageProof(resp, masks), nil
		}
		if params.Annotate != nil && !*params.Annotate {
			data, err := sess.Screenshot(params.FullPage)
			if err != nil {
				return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
			}
			resp := fantasy.NewImageResponse(data, "image/png")
			resp.Content = "Clean screenshot without target annotations. No pointer viewport grant was issued; request an annotated viewport screenshot before coordinate input."
			return resp, nil
		}
		if input, ok := sess.(browser.InputSession); ok && !params.FullPage {
			resp, err := viewportScreenshotResponse(ctx, input)
			if err == nil && !resp.IsError {
				grantScreenshotRefs(ctx, browserStateID(ctx), sess)
			}
			return resp, err
		}
		data, err := sess.AnnotatedScreenshot(params.FullPage)
		if err != nil {
			return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
		}
		grantScreenshotRefs(ctx, browserStateID(ctx), sess)
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
