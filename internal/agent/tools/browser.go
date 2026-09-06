package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/template"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
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
	"screenshot", "url", "close",
}

// browserReadOnlyActions are the actions that only observe the current page
// (or the manager's bookkeeping) rather than changing what's loaded or
// submitting anything -- these get Safe: true, the same way bash marks its
// read-only command allowlist, so ModeAutoAcceptEdits doesn't stop to ask
// about them while ModePlan still denies the tool outright.
var browserReadOnlyActions = map[string]bool{
	"scroll":     true,
	"text":       true,
	"html":       true,
	"snapshot":   true,
	"images":     true,
	"console":    true,
	"screenshot": true,
	"url":        true,
	"close":      true,
}

type BrowserParams struct {
	Action string `json:"action" description:"One of: navigate, back, forward, click, type, key, scroll, eval, text, html, snapshot, images, console, dialog, cdp, screenshot, url, close. See the tool description for what each needs and returns."`
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
	Action     string          `json:"action"`
	URL        string          `json:"url,omitempty"`
	Selector   string          `json:"selector,omitempty"`
	Ref        string          `json:"ref,omitempty"`
	Text       string          `json:"text,omitempty"`
	Key        string          `json:"key,omitempty"`
	Direction  string          `json:"direction,omitempty"`
	Amount     int             `json:"amount,omitempty"`
	Script     string          `json:"script,omitempty"`
	Full       bool            `json:"full,omitempty"`
	FullPage   bool            `json:"full_page,omitempty"`
	Accept     bool            `json:"accept,omitempty"`
	PromptText string          `json:"prompt_text,omitempty"`
	CDPMethod  string          `json:"cdp_method,omitempty"`
	CDPParams  json.RawMessage `json:"cdp_params,omitempty"`
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

func NewBrowserTool(permissions permission.Service, workingDir string, cfg config.ToolBrowser) fantasy.AgentTool {
	manager := browser.GetManager(browser.Options{
		ExecutablePath: cfg.ExecutablePath,
		Headless:       cfg.IsHeadless(),
		UserDataDir:    cfg.GetUserDataDir(),
		UseRealProfile: cfg.UsesRealProfile(),
		RealProfilePin: cfg.GetRealProfilePin(),
		RemoteURL:      cfg.GetRemoteURL(),
		ActionTimeout:  cfg.GetActionTimeout(),
		IdleTimeout:    cfg.GetIdleTimeout(),
	})
	return newBrowserTool(permissions, workingDir, manager, browserDescription(cfg.UsesRealProfile()))
}

func newBrowserTool(permissions permission.Service, workingDir string, sessions browserSessions, description string) fantasy.AgentTool {
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

			if action == "close" {
				sessions.Close(sessionID)
				return fantasy.NewTextResponse("Browser session closed."), nil
			}

			sess, err := sessions.Session(sessionID)
			if err != nil {
				return fantasy.NewTextErrorResponse("failed to start browser: " + err.Error()), nil
			}

			return runBrowserAction(sess, action, params)
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
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Navigated to "+params.URL), metadata), nil

	case "back":
		if err := sess.Back(); err != nil {
			return fantasy.NewTextErrorResponse("back failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Navigated back."), metadata), nil

	case "forward":
		if err := sess.Forward(); err != nil {
			return fantasy.NewTextErrorResponse("forward failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Navigated forward."), metadata), nil

	case "click":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Click(selector); err != nil {
			return fantasy.NewTextErrorResponse("click failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Clicked "+describeTarget(params)), metadata), nil

	case "type":
		selector, err := resolveTargetSelector(action, params)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if err := sess.Type(selector, params.Text); err != nil {
			return fantasy.NewTextErrorResponse("type failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Typed into "+describeTarget(params)), metadata), nil

	case "key":
		if params.Key == "" {
			return fantasy.NewTextErrorResponse("key is required for the key action"), nil
		}
		if err := sess.PressKey(strings.ToLower(params.Key)); err != nil {
			return fantasy.NewTextErrorResponse("key press failed: " + err.Error()), nil
		}
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Sent key "+params.Key), metadata), nil

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
		data, err := sess.Screenshot(params.FullPage)
		if err != nil {
			return fantasy.NewTextErrorResponse("screenshot failed: " + err.Error()), nil
		}
		return fantasy.NewImageResponse(data, "image/png"), nil

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
