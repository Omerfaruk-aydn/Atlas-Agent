// Package browser drives a real Chrome/Chromium instance over the Chrome
// DevTools Protocol (via chromedp) so the agent's browser tool can navigate,
// click, type, run JavaScript, and take screenshots against a live page --
// the kind of UI verification a headless test suite can't do (visual review,
// exploring a site with no API, following a login flow interactively).
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Options configures how new sessions are launched and reaped. The
// process-wide manager re-reads them on every GetManager call, so a
// setting the user changes mid-session takes effect on the next launch
// rather than at the next restart.
type Options struct {
	// ExecutablePath is the Chrome/Chromium binary to launch. Empty lets
	// chromedp search the usual install locations.
	ExecutablePath string
	// Headless runs the browser without a visible window.
	Headless bool
	// UserDataDir is the Chrome profile directory to launch with. A
	// profile that persists is what lets the agent act as a signed-in
	// user: cookies written on one run are still there on the next.
	// Empty gets chromedp's throwaway profile, which starts signed out
	// of everything, every time.
	UserDataDir string
	// UseRealProfile copies the user's own Chrome profile into
	// UserDataDir before launching, so the agent starts signed into
	// whatever the user is signed into. See profile.go for why it is a
	// copy and what that costs.
	UseRealProfile bool
	// RealProfilePin names which profile to copy on a machine with
	// several ("Default", "Profile 2"). Empty copies the one the user
	// browsed with last.
	RealProfilePin string
	// RemoteURL is the DevTools endpoint of a browser someone else
	// started (say http://127.0.0.1:9222). Set, it is driven instead of
	// launching one, and ExecutablePath, Headless and UserDataDir no
	// longer apply -- they describe a launch that no longer happens.
	RemoteURL string
	// ActionTimeout bounds a single action (navigate, click, eval...).
	ActionTimeout time.Duration
	// IdleTimeout is how long an unused session is kept open before a
	// later call reaps it. Zero disables reaping.
	IdleTimeout time.Duration
}

// Session drives one open browser tab, kept alive across calls so a
// multi-step flow (navigate, then click, then read the result) operates on
// the same page instead of a fresh one each time.
type Session interface {
	Navigate(url string) error
	Back() error
	Forward() error
	Click(selector string) error
	Type(selector, text string) error
	PressKey(name string) error
	Scroll(dx, dy int) error
	Eval(expression string) (string, error)
	Text(selector string) (string, error)
	HTML(selector string) (string, error)
	Screenshot(fullPage bool) ([]byte, error)
	URL() (string, error)
	// Snapshot returns every currently visible interactive element (or,
	// with full, every one in the document regardless of scroll
	// position), each tagged with a stable ref an action can target
	// instead of a hand-written CSS selector. Refs are assigned lazily
	// and persist across snapshots of the same page, but do not survive
	// a navigation -- the DOM they were attached to is gone.
	Snapshot(full bool) ([]SnapshotElement, error)
	// Images lists every <img> on the page, for finding something to
	// look at more closely (e.g. with Screenshot or an external vision
	// call) rather than guessing a selector.
	Images() ([]ImageInfo, error)
	// ConsoleLogs returns console API calls and uncaught exceptions
	// observed since the session opened, oldest first, bounded to the
	// most recent maxConsoleEntries.
	ConsoleLogs() []ConsoleEntry
	// PendingDialogs returns native JavaScript dialogs (alert/confirm/
	// prompt/beforeunload) waiting on a response, oldest first. A
	// pending dialog blocks the page -- and every subsequent action --
	// until HandleDialog answers it.
	PendingDialogs() []DialogInfo
	// HandleDialog answers the oldest pending dialog. promptText is
	// used only for a prompt() dialog being accepted; ignored otherwise.
	// Returns an error if nothing is pending.
	HandleDialog(accept bool, promptText string) error
	// RawCDP sends a Chrome DevTools Protocol command not covered by
	// the methods above -- an escape hatch, not the common path. See
	// https://chromedevtools.github.io/devtools-protocol/ for method
	// names and parameter shapes.
	RawCDP(method string, params map[string]any) (map[string]any, error)
	// Close releases the underlying browser process. Safe to call more
	// than once.
	Close()
}

// SnapshotElement is one interactive element found by Session.Snapshot.
type SnapshotElement struct {
	Ref   string `json:"ref"`
	Role  string `json:"role"`
	Tag   string `json:"tag"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// ImageInfo is one <img> found by Session.Images.
type ImageInfo struct {
	Src string `json:"src"`
	Alt string `json:"alt,omitempty"`
}

// ConsoleEntry is one console API call or uncaught exception captured by
// Session.ConsoleLogs.
type ConsoleEntry struct {
	Type string    `json:"type"` // log, warning, error, info, debug, exception, ...
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

// DialogInfo is one native JavaScript dialog waiting on Session.HandleDialog.
type DialogInfo struct {
	Type          string `json:"type"` // alert, confirm, prompt, beforeunload
	Message       string `json:"message"`
	DefaultPrompt string `json:"default_prompt,omitempty"`
}

// maxConsoleEntries and maxPendingDialogs bound the in-memory buffers a
// long-lived session accumulates, the same way shell's background job
// output is capped -- a chatty page must not grow a session's memory
// without limit.
const (
	maxConsoleEntries = 200
	maxPendingDialogs = 20
)

// snapshotScript walks the DOM for interactive elements and returns
// them as a JSON array of {ref, role, tag, name, value}. A ref is a
// short, stable id (data-atlas-ref="e3") assigned the first time an
// element is seen and reused on every later snapshot of the same page
// -- it does not survive a navigation, since that replaces the DOM the
// attribute was attached to. %v is a Go bool literal (true/false)
// selecting whether elements currently scrolled out of the viewport are
// included.
const snapshotScript = `(function(full) {
	var counter = window.__atlasRefCounter || 0;
	var out = [];
	var seen = new Set();
	var candidates = document.querySelectorAll(
		'a[href], button, input, textarea, select, [role], [contenteditable=""], [contenteditable="true"], [onclick], [tabindex]'
	);
	var vw = window.innerWidth, vh = window.innerHeight;
	for (var i = 0; i < candidates.length; i++) {
		var el = candidates[i];
		if (seen.has(el)) continue;
		seen.add(el);
		var style = window.getComputedStyle(el);
		if (style.display === 'none' || style.visibility === 'hidden') continue;
		var rect = el.getBoundingClientRect();
		if (rect.width === 0 || rect.height === 0) continue;
		if (!full && (rect.bottom < 0 || rect.top > vh || rect.right < 0 || rect.left > vw)) continue;

		var ref = el.getAttribute('data-atlas-ref');
		if (!ref) {
			counter++;
			ref = 'e' + counter;
			el.setAttribute('data-atlas-ref', ref);
		}

		var tag = el.tagName.toLowerCase();
		var role = el.getAttribute('role');
		if (!role) {
			if (tag === 'a') role = 'link';
			else if (tag === 'select') role = 'combobox';
			else if (tag === 'textarea') role = 'textbox';
			else if (tag === 'input') {
				var t = (el.getAttribute('type') || 'text').toLowerCase();
				role = (t === 'checkbox' || t === 'radio') ? t : (t === 'submit' || t === 'button') ? 'button' : 'textbox';
			} else {
				role = tag;
			}
		}

		var name = el.getAttribute('aria-label') || el.getAttribute('placeholder') ||
			el.getAttribute('alt') || el.getAttribute('title') || '';
		if (!name) {
			var labelledby = el.getAttribute('aria-labelledby');
			var lbl = labelledby && document.getElementById(labelledby);
			if (lbl) name = lbl.innerText;
		}
		if (!name && el.labels && el.labels.length) name = el.labels[0].innerText;
		if (!name) name = el.innerText || '';
		name = name.replace(/\s+/g, ' ').trim().slice(0, 120);

		var value = '';
		if (tag === 'input' || tag === 'textarea' || tag === 'select') value = el.value || '';

		out.push({ref: ref, role: role, tag: tag, name: name, value: value});
	}
	window.__atlasRefCounter = counter;
	return JSON.stringify(out);
})(%v)`

// imagesScript lists every <img> on the page as a JSON array of
// {src, alt}, capped so a page with thousands of images does not
// flood the response.
const imagesScript = `JSON.stringify(Array.from(document.images).slice(0, 200).map(function(img) {
	return {src: img.src, alt: img.alt || ''};
}))`

// namedKeys maps the tool's friendly key names to chromedp/kb's control
// character encoding for KeyEvent.
var namedKeys = map[string]string{
	"enter":      kb.Enter,
	"tab":        kb.Tab,
	"escape":     kb.Escape,
	"backspace":  kb.Backspace,
	"delete":     kb.Delete,
	"arrowup":    kb.ArrowUp,
	"arrowdown":  kb.ArrowDown,
	"arrowleft":  kb.ArrowLeft,
	"arrowright": kb.ArrowRight,
}

// ResolveKey translates a friendly key name (case-insensitive) into the
// encoding PressKey expects, and reports whether it was recognized.
func ResolveKey(name string) (string, bool) {
	k, ok := namedKeys[name]
	return k, ok
}

// SupportedKeys lists the key names ResolveKey accepts, for error messages.
func SupportedKeys() []string {
	names := make([]string, 0, len(namedKeys))
	for name := range namedKeys {
		names = append(names, name)
	}
	return names
}

type chromedpSession struct {
	ctx           context.Context
	cancel        context.CancelFunc
	actionTimeout time.Duration
	closeOnce     sync.Once

	// mu guards console and dialogs, which the CDP event listener
	// (chromedp.ListenTarget's callback, invoked synchronously and
	// concurrently with whatever action is in flight) appends to.
	mu      sync.Mutex
	console []ConsoleEntry
	dialogs []DialogInfo
}

func newChromedpSession(opts Options) (Session, error) {
	var (
		allocCtx    context.Context
		allocCancel context.CancelFunc
	)
	if opts.RemoteURL != "" {
		if err := ensureRemoteBrowser(opts); err != nil {
			return nil, err
		}
		allocCtx, allocCancel = chromedp.NewRemoteAllocator(context.Background(), opts.RemoteURL)
	} else {
		allocOpts := append(append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...),
			chromedp.Flag("headless", opts.Headless),
		)
		if opts.ExecutablePath != "" {
			allocOpts = append(allocOpts, chromedp.ExecPath(opts.ExecutablePath))
		}
		if opts.UserDataDir != "" {
			// chromedp deletes the profile it made itself; naming one
			// here also tells it to leave the directory alone, which is
			// the whole point -- the logins have to still be there next
			// time.
			allocOpts = append(allocOpts, chromedp.UserDataDir(opts.UserDataDir))
		}
		allocCtx, allocCancel = chromedp.NewExecAllocator(context.Background(), allocOpts...)
	}
	ctx, cancel := chromedp.NewContext(allocCtx)

	// Launch now so a missing browser binary or launch failure surfaces
	// here, at session creation, instead of on the caller's first action.
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		allocCancel()
		return nil, fmt.Errorf("failed to launch browser: %w", err)
	}

	s := &chromedpSession{
		ctx:           ctx,
		cancel:        func() { cancel(); allocCancel() },
		actionTimeout: opts.ActionTimeout,
	}

	// Runtime and Page must be explicitly enabled for their events
	// (console calls, exceptions, dialog-opening) to fire at all --
	// enabling is otherwise implicit only for the actions (Navigate,
	// Click, ...) that need it internally.
	if err := s.run(runtime.Enable(), page.Enable()); err != nil {
		s.cancel()
		return nil, fmt.Errorf("failed to enable browser event reporting: %w", err)
	}

	// Registered once, for the session's whole lifetime: chromedp
	// requires this run synchronously and non-blocking (see
	// ListenTarget's doc), so it only ever appends to the buffers below
	// -- the actual HandleJavaScriptDialog response happens later, in
	// its own ordinary s.run call triggered by the dialog tool action.
	chromedp.ListenTarget(ctx, s.handleTargetEvent)

	return s, nil
}

// handleTargetEvent is chromedp's synchronous, non-blocking event
// callback (see chromedp.ListenTarget) -- it must never call an Action
// itself, only record what happened for a later call to read.
func (s *chromedpSession) handleTargetEvent(ev any) {
	switch ev := ev.(type) {
	case *runtime.EventConsoleAPICalled:
		s.appendConsole(ConsoleEntry{Type: string(ev.Type), Text: formatConsoleArgs(ev.Args), Time: time.Now()})
	case *runtime.EventExceptionThrown:
		text := "uncaught exception"
		if ev.ExceptionDetails != nil {
			text = ev.ExceptionDetails.Error()
		}
		s.appendConsole(ConsoleEntry{Type: "exception", Text: text, Time: time.Now()})
	case *page.EventJavascriptDialogOpening:
		s.appendDialog(DialogInfo{Type: string(ev.Type), Message: ev.Message, DefaultPrompt: ev.DefaultPrompt})
	}
}

func (s *chromedpSession) appendConsole(entry ConsoleEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.console = append(s.console, entry)
	if len(s.console) > maxConsoleEntries {
		s.console = s.console[len(s.console)-maxConsoleEntries:]
	}
}

func (s *chromedpSession) appendDialog(d DialogInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialogs = append(s.dialogs, d)
	if len(s.dialogs) > maxPendingDialogs {
		s.dialogs = s.dialogs[len(s.dialogs)-maxPendingDialogs:]
	}
}

// formatConsoleArgs renders a console call's arguments the way a
// browser devtools panel would: each argument's literal value when
// available, else its object description.
func formatConsoleArgs(args []*runtime.RemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			continue
		}
		switch {
		case len(a.Value) > 0:
			parts = append(parts, strings.Trim(string(a.Value), `"`))
		case a.Description != "":
			parts = append(parts, a.Description)
		case a.ClassName != "":
			parts = append(parts, a.ClassName)
		}
	}
	return strings.Join(parts, " ")
}

func (s *chromedpSession) run(actions ...chromedp.Action) error {
	timeout := s.actionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	return chromedp.Run(runCtx, actions...)
}

func (s *chromedpSession) Navigate(url string) error {
	return s.run(chromedp.Navigate(url))
}

func (s *chromedpSession) Click(selector string) error {
	return s.run(chromedp.Click(selector, chromedp.ByQuery))
}

func (s *chromedpSession) Type(selector, text string) error {
	return s.run(
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

func (s *chromedpSession) PressKey(name string) error {
	key, ok := ResolveKey(name)
	if !ok {
		return fmt.Errorf("unsupported key %q (supported: %v)", name, SupportedKeys())
	}
	return s.run(chromedp.KeyEvent(key))
}

func (s *chromedpSession) Back() error {
	return s.run(chromedp.NavigateBack())
}

func (s *chromedpSession) Forward() error {
	return s.run(chromedp.NavigateForward())
}

func (s *chromedpSession) Scroll(dx, dy int) error {
	return s.run(chromedp.Evaluate(fmt.Sprintf("window.scrollBy(%d, %d)", dx, dy), nil))
}

func (s *chromedpSession) Eval(expression string) (string, error) {
	var result string
	if err := s.run(chromedp.Evaluate(expression, &result)); err != nil {
		return "", err
	}
	return result, nil
}

func (s *chromedpSession) Text(selector string) (string, error) {
	var result string
	if err := s.run(chromedp.Text(selector, &result, chromedp.ByQuery)); err != nil {
		return "", err
	}
	return result, nil
}

func (s *chromedpSession) HTML(selector string) (string, error) {
	var result string
	if err := s.run(chromedp.OuterHTML(selector, &result, chromedp.ByQuery)); err != nil {
		return "", err
	}
	return result, nil
}

func (s *chromedpSession) Screenshot(fullPage bool) ([]byte, error) {
	var buf []byte
	if fullPage {
		if err := s.run(chromedp.FullScreenshot(&buf, 90)); err != nil {
			return nil, err
		}
		return buf, nil
	}
	if err := s.run(chromedp.CaptureScreenshot(&buf)); err != nil {
		return nil, err
	}
	return buf, nil
}

func (s *chromedpSession) URL() (string, error) {
	var url string
	if err := s.run(chromedp.Location(&url)); err != nil {
		return "", err
	}
	return url, nil
}

func (s *chromedpSession) Snapshot(full bool) ([]SnapshotElement, error) {
	var raw string
	if err := s.run(chromedp.Evaluate(fmt.Sprintf(snapshotScript, full), &raw)); err != nil {
		return nil, err
	}
	var elements []SnapshotElement
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return nil, fmt.Errorf("parsing snapshot: %w", err)
	}
	return elements, nil
}

func (s *chromedpSession) Images() ([]ImageInfo, error) {
	var raw string
	if err := s.run(chromedp.Evaluate(imagesScript, &raw)); err != nil {
		return nil, err
	}
	var images []ImageInfo
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return nil, fmt.Errorf("parsing image list: %w", err)
	}
	return images, nil
}

func (s *chromedpSession) ConsoleLogs() []ConsoleEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ConsoleEntry(nil), s.console...)
}

func (s *chromedpSession) PendingDialogs() []DialogInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DialogInfo(nil), s.dialogs...)
}

func (s *chromedpSession) HandleDialog(accept bool, promptText string) error {
	s.mu.Lock()
	if len(s.dialogs) == 0 {
		s.mu.Unlock()
		return errors.New("no pending dialog to handle")
	}
	// FIFO: dialogs are answered in the order they opened, matching how
	// the page actually processes them (a second alert() does not open
	// until the first is dismissed).
	s.dialogs = s.dialogs[1:]
	s.mu.Unlock()

	return s.run(chromedp.ActionFunc(func(ctx context.Context) error {
		return page.HandleJavaScriptDialog(accept).WithPromptText(promptText).Do(ctx)
	}))
}

func (s *chromedpSession) RawCDP(method string, params map[string]any) (map[string]any, error) {
	var result map[string]any
	err := s.run(chromedp.ActionFunc(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Target == nil {
			return errors.New("no active browser target")
		}
		return c.Target.Execute(ctx, method, params, &result)
	}))
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *chromedpSession) Close() {
	s.closeOnce.Do(s.cancel)
}

// newSessionFunc lets tests substitute a fake driver instead of launching a
// real browser process.
type newSessionFunc func(Options) (Session, error)

// Manager keeps at most one open Session per chat session ID, so a
// multi-step browser flow within a turn (and across turns of the same
// session) reuses the same tab instead of relaunching Chrome each call.
type Manager struct {
	mu         sync.Mutex
	opts       Options
	newSession newSessionFunc
	sessions   map[string]Session
	lastUsed   map[string]time.Time
}

func newManager(opts Options, newSession newSessionFunc) *Manager {
	return &Manager{
		opts:       opts,
		newSession: newSession,
		sessions:   map[string]Session{},
		lastUsed:   map[string]time.Time{},
	}
}

var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
)

// GetManager returns the process-wide browser manager, built on first
// use and kept for the life of the process so a multi-step flow survives
// the agent's tool list being reassembled mid-session -- as
// shell.GetBackgroundShellManager does for the bash tools.
//
// Every call re-applies opts, because reassembling that tool list is
// exactly how a changed setting arrives. Without this the first opts
// seen won the process, and turning the visible window on or off did
// nothing until the next restart.
func GetManager(opts Options) *Manager {
	defaultManagerOnce.Do(func() {
		defaultManager = newManager(opts, newChromedpSession)
	})
	defaultManager.setOptions(opts)
	return defaultManager
}

// CloseAllSessions closes every open session if a manager was ever
// started, and does nothing if none was. Shutdown uses it rather than
// GetManager so tearing down cannot overwrite the running options with
// a zero value on the way out.
func CloseAllSessions() {
	if defaultManager == nil {
		return
	}
	defaultManager.CloseAll()
}

// setOptions adopts opts for sessions launched from here on. A change to
// how the browser is launched cannot reach a process already running, so
// open sessions are closed instead of left contradicting the setting:
// the next action relaunches under what the user actually chose.
func (m *Manager) setOptions(opts Options) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.opts == opts {
		return
	}
	relaunch := m.opts.Headless != opts.Headless ||
		m.opts.ExecutablePath != opts.ExecutablePath ||
		m.opts.UserDataDir != opts.UserDataDir ||
		m.opts.RemoteURL != opts.RemoteURL ||
		m.opts.UseRealProfile != opts.UseRealProfile ||
		m.opts.RealProfilePin != opts.RealProfilePin
	m.opts = opts
	if !relaunch {
		return
	}
	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
		delete(m.lastUsed, id)
	}
}

// Session returns the open session for id, launching one if none exists.
func (m *Manager) Session(id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reapLocked()

	if s, ok := m.sessions[id]; ok {
		m.lastUsed[id] = time.Now()
		return s, nil
	}

	s, err := m.newSession(m.opts)
	if err != nil {
		return nil, err
	}
	m.sessions[id] = s
	m.lastUsed[id] = time.Now()
	return s, nil
}

// Close closes the session for id, if one is open. Safe to call when none
// is.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Close()
		delete(m.sessions, id)
		delete(m.lastUsed, id)
	}
}

// CloseAll closes every open session. Called on process shutdown so a
// headless Chrome instance never outlives the agent that launched it.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
		delete(m.lastUsed, id)
	}
}

// reapLocked closes and drops sessions idle for longer than opts.IdleTimeout.
// Called with mu held, on every Session lookup rather than off a background
// goroutine, so a manager nobody is using holds no timers.
func (m *Manager) reapLocked() {
	if m.opts.IdleTimeout <= 0 {
		return
	}
	cutoff := time.Now().Add(-m.opts.IdleTimeout)
	for id, last := range m.lastUsed {
		if last.Before(cutoff) {
			if s, ok := m.sessions[id]; ok {
				s.Close()
			}
			delete(m.sessions, id)
			delete(m.lastUsed, id)
		}
	}
}
