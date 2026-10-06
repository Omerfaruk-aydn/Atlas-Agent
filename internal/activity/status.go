package activity

import (
	"fmt"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// statusCategory is the recorded event a caption describes. Captions vary
// only within a category, so variety never claims work that did not happen.
type statusCategory uint8

const (
	statusWorking statusCategory = iota
	statusThinking
	statusOpening
	statusLaunching
	statusReading
	statusObserving
	statusFinding
	statusTyping
	statusForm
	statusChoosing
	statusClicking
	statusScrolling
	statusKeys
	statusPointer
	statusDragging
	statusSaving
	statusUploading
	statusVerifying
	statusWaitingPage
	statusTabs
	statusAwaitQuestion
	statusAwaitPermission
	statusDelivering
	statusResuming
	statusDenied
	statusFailed
	statusDone
)

// statusPhrase is an Atlas-authored caption; weight sets how often it is
// chosen, so characterful variants stay occasional.
type statusPhrase struct {
	text   string
	weight int
}

// statusCatalog holds the English source captions; i18n translates them.
var statusCatalog = map[statusCategory][]statusPhrase{
	statusWorking:   {{"Working", 4}, {"Working on it", 3}, {"Carrying on", 2}, {"Handling the step", 2}},
	statusThinking:  {{"Thinking", 6}, {"Planning", 4}, {"Planning the next step", 3}, {"Considering options", 3}, {"Preparing", 3}, {"Working it out", 2}, {"Cooking", 1}, {"Brewing an idea", 1}},
	statusOpening:   {{"Opening the page", 5}, {"Loading the page", 3}, {"Going to the page", 3}, {"Navigating", 3}},
	statusLaunching: {{"Opening the app", 5}, {"Starting the app", 3}, {"Bringing up the window", 2}},
	statusReading:   {{"Reading the content", 5}, {"Looking at the page", 4}, {"Scanning the screen", 3}, {"Reviewing what is shown", 2}},
	statusObserving: {{"Observing again", 5}, {"Taking another look", 4}, {"Re-reading the screen", 3}},
	statusFinding:   {{"Finding the target", 5}, {"Locating the element", 4}, {"Searching the page", 3}},
	statusTyping:    {{"Typing", 6}, {"Entering text", 4}, {"Writing in the field", 2}},
	statusForm:      {{"Filling in the form", 5}, {"Setting the value", 3}, {"Entering the value", 2}},
	statusChoosing:  {{"Choosing an option", 5}, {"Selecting from the list", 3}},
	statusClicking:  {{"Clicking", 6}, {"Selecting the target", 3}, {"Pressing", 2}},
	statusScrolling: {{"Scrolling", 6}, {"Moving through the page", 3}, {"Scrolling to the content", 2}},
	statusKeys:      {{"Pressing keys", 5}, {"Sending a shortcut", 3}},
	statusPointer:   {{"Moving the pointer", 5}, {"Positioning the pointer", 3}},
	statusDragging:  {{"Dragging", 5}, {"Dragging the item", 3}},
	statusSaving:    {{"Saving the file", 5}, {"Downloading", 4}, {"Saving the download", 2}},
	statusUploading: {{"Uploading the file", 5}, {"Attaching the file", 3}},
	statusVerifying: {{"Verifying the result", 5}, {"Checking the result", 4}, {"Confirming the state", 3}},
	statusWaitingPage: {
		{"Waiting for the page", 5}, {"Letting it load", 3}, {"Waiting for a change", 2},
	},
	statusTabs:            {{"Managing tabs", 5}, {"Working with tabs", 3}},
	statusAwaitQuestion:   {{"Waiting for your answer", 6}, {"Your answer is needed", 3}, {"A question for you", 2}},
	statusAwaitPermission: {{"Waiting for your permission", 6}, {"Needs your approval", 3}, {"Paused for your decision", 2}},
	statusDelivering:      {{"Sending your answer", 5}, {"Delivering your decision", 3}},
	statusResuming:        {{"Continuing", 6}, {"Resuming the task", 3}, {"Picking up where it left off", 2}},
	statusDenied:          {{"Permission declined", 5}, {"Skipping the declined action", 3}, {"Respecting your decision", 2}},
	statusFailed:          {{"Investigating the problem", 5}, {"Looking into the error", 4}, {"Checking what went wrong", 3}},
	statusDone:            {{"Done", 6}, {"Finished", 3}, {"All done", 2}},
}

// statusCategoryFor maps recorded work state and tool action to a category.
func statusCategoryFor(e Event) statusCategory {
	switch e.State {
	case StateAwaitQuestion:
		return statusAwaitQuestion
	case StateAwaitPermission:
		return statusAwaitPermission
	case StateDelivering:
		return statusDelivering
	case StateResuming:
		return statusResuming
	case StateDenied:
		return statusDenied
	case StateFailed:
		return statusFailed
	case StateDone:
		return statusDone
	case StateThinking, StateActive:
		return statusThinking
	}
	switch e.Action {
	case "navigate", "back", "forward", "url":
		return statusOpening
	case "launch_app", "focus":
		return statusLaunching
	case "text", "html", "snapshot", "screenshot", "images", "console", "inspect", "ocr", "capture_region", "capture_window", "windows", "monitors", "frames", "network", "tabs", "screen_size", "cursor_position":
		if e.AfterFailure {
			return statusObserving
		}
		return statusReading
	case "find":
		return statusFinding
	case "type", "semantic_type", "vault_fill", "auth_code":
		return statusTyping
	case "set_value":
		return statusForm
	case "select":
		return statusChoosing
	case "click", "double_click", "right_click", "invoke", "semantic_click":
		return statusClicking
	case "scroll", "up", "down", "left", "right":
		return statusScrolling
	case "key", "hotkey":
		return statusKeys
	case "move":
		return statusPointer
	case "drag":
		return statusDragging
	case "download_start", "download_wait":
		return statusSaving
	case "upload":
		return statusUploading
	case "assert":
		return statusVerifying
	case "wait", "wait_for", "dialog_wait", "popup_wait":
		return statusWaitingPage
	case "tab_new", "tab_select", "tab_close":
		return statusTabs
	}
	return statusWorking
}

// Captions hold for this long within one category, so text never flickers;
// a category change updates at once.
const statusDwell = 7 * time.Second

// statusPicker chooses captions for one surface without repeating the
// previous caption back to back.
type statusPicker struct {
	category statusCategory
	index    int
	at       time.Time
	seed     uint64
	ready    bool
}

func (p *statusPicker) next(phrases []statusPhrase) int {
	total := 0
	for i, phrase := range phrases {
		if p.ready && i == p.index && len(phrases) > 1 {
			continue
		}
		total += phrase.weight
	}
	p.seed ^= p.seed << 13
	p.seed ^= p.seed >> 7
	p.seed ^= p.seed << 17
	roll := int(p.seed % uint64(max(1, total)))
	for i, phrase := range phrases {
		if p.ready && i == p.index && len(phrases) > 1 {
			continue
		}
		if roll < phrase.weight {
			return i
		}
		roll -= phrase.weight
	}
	return 0
}

// text returns the caption for e at now, in e's language.
func (p *statusPicker) text(e Event, now time.Time) string {
	category := statusCategoryFor(e)
	phrases := statusCatalog[category]
	if p.seed == 0 {
		p.seed = uint64(now.UnixNano()) | 1
	}
	switch {
	case !p.ready || category != p.category:
		// A new category starts with its plainest caption.
		p.category, p.index, p.at, p.ready = category, 0, now, true
	case now.Sub(p.at) >= statusDwell:
		p.index, p.at = p.next(phrases), now
	}
	return i18n.Text(e.Language, phrases[p.index].text)
}

// StatusPicker chooses captions for one surface; it is safe for concurrent
// use by renderers outside this package.
type StatusPicker struct {
	mu sync.Mutex
	p  statusPicker
}

// Text returns the caption for e at now.
func (s *StatusPicker) Text(e Event, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p.text(e, now)
}

// FormatElapsed renders run time as 00:12, 01:48 or 1:02:03.
func FormatElapsed(d time.Duration) string {
	d = max(0, d).Truncate(time.Second)
	h, m, s := int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// Elapsed is the run's total time on a monotonic clock, frozen at its end.
func (e Event) Elapsed(now time.Time) time.Duration {
	if e.RunStarted.IsZero() {
		return 0
	}
	if !e.RunEnded.IsZero() {
		return e.RunEnded.Sub(e.RunStarted)
	}
	return now.Sub(e.RunStarted)
}
