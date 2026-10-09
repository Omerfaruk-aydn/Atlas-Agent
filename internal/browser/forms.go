package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// FormFieldResult is one field of a fill_form run.
type FormFieldResult struct {
	Index   int          `json:"index"`
	Status  string       `json:"status"`
	Skipped bool         `json:"skipped,omitempty"`
	Error   string       `json:"error,omitempty"`
	Field   *FieldResult `json:"field,omitempty"`
	Valid   *bool        `json:"valid,omitempty"`
	Message string       `json:"validation_message,omitempty"`
}

// FormResult separates filling from validation: every field reports the
// value the page kept and whether the page considers it valid. Filling is
// not submission and submission is not acceptance.
type FormResult struct {
	Fields    []FormFieldResult `json:"fields"`
	Applied   int               `json:"applied"`
	Skipped   int               `json:"skipped"`
	Failed    int               `json:"failed"`
	Invalid   int               `json:"invalid"`
	Submitted bool              `json:"submitted"`
	Next      string            `json:"next"`
}

// formFieldRequest builds one field's request. The form's frame, document
// and tab pins apply to every field unless a field names its own frame.
func formFieldRequest(f FormField, p Request) Request {
	r := Request{
		Selector: f.Selector, Role: f.Role, Name: f.Name, Label: f.Label, Within: f.Within, Value: f.Value, Commit: f.Commit, TimeoutMS: p.TimeoutMS,
		FrameID: p.FrameID, DocumentID: p.DocumentID, ExpectedTabID: p.ExpectedTabID, ExpectedOrigin: p.ExpectedOrigin,
	}
	if f.Value == "" {
		// An explicit empty value clears the field.
		r.Mode = "clear"
	}
	if f.FrameID != "" && f.FrameID != p.FrameID {
		r.FrameID, r.DocumentID = f.FrameID, ""
	}
	if r.Selector == "" && f.Ref != "" {
		r.Selector = fmt.Sprintf(`[data-atlas-ref="%s"]`, f.Ref)
	}
	return r
}

// FillForm sets fields in order with commit and readback, then reads every
// field's validity. With OnlyInvalid, fields already holding their value
// and passing validation are left untouched, so a correction pass changes
// only what the page rejected.
func (s *chromedpSession) FillForm(ctx context.Context, p Request) (FormResult, error) {
	if len(p.Fields) == 0 || len(p.Fields) > 40 {
		return FormResult{}, errors.New("fill_form requires 1-40 fields")
	}
	for i, f := range p.Fields {
		if f.Ref != "" && (len(f.Ref) > 32 || containsAny(f.Ref, `"[]`)) {
			return FormResult{}, fmt.Errorf("field %d: invalid ref", i)
		}
		if f.Selector == "" && f.Ref == "" && f.Role == "" && f.Name == "" && f.Label == "" {
			return FormResult{}, fmt.Errorf("field %d: target is required", i)
		}
		if f.Kind != "" && f.Kind != "field" && f.Kind != "option" {
			return FormResult{}, fmt.Errorf("field %d: kind must be field or option", i)
		}
	}
	result := FormResult{}
	// Pin every participating frame before sending the first input. Binding
	// a later field after navigation would silently target a new document.
	bindCtx, cancel, err := s.fieldContext(ctx, p)
	if err != nil {
		return result, err
	}
	defer cancel()
	pinned := make([]Request, len(p.Fields))
	frames := make(map[string]fieldFrame)
	for i, f := range p.Fields {
		req := formFieldRequest(f, p)
		frame, exists := frames[req.FrameID]
		if !exists {
			frame, err = s.bindFieldFrame(bindCtx, req)
			if err != nil {
				return result, fieldError(ctx, err)
			}
			frames[req.FrameID] = frame
		}
		if req.DocumentID != "" && req.DocumentID != frame.loaderID {
			return result, errors.New("stale_document: the form's document changed; nothing was written")
		}
		req.DocumentID, req.ExpectedTabID = frame.loaderID, frame.tabID
		pinned[i] = req
	}
	stop := func(index int, cause error) (FormResult, error) {
		for j := index; j < len(p.Fields); j++ {
			result.Fields = append(result.Fields, FormFieldResult{Index: j, Status: "not_attempted", Error: "Form context changed; no input was sent to this field."})
		}
		result.Next = "Form filling stopped because its context changed. Observe the intended page before continuing; earlier inputs may already have been applied."
		return result, fieldError(ctx, cause)
	}
	verify := func() error {
		verifyCtx, done, err := s.fieldContext(ctx, p)
		if err != nil {
			return err
		}
		defer done()
		for _, frame := range frames {
			if err := s.verifyFieldDocument(verifyCtx, frame); err != nil {
				return err
			}
		}
		return nil
	}
	for i, f := range p.Fields {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := verify(); err != nil {
			return stop(i, err)
		}
		req := pinned[i]
		entry := FormFieldResult{Index: i}
		if p.OnlyInvalid {
			current, err := s.ReadField(ctx, req)
			if err == nil && (current.Valid == nil || *current.Valid) && formFieldHolds(f, current) {
				entry.Status, entry.Skipped = "unchanged", true
				entry.Field = &current
				result.Skipped++
				result.Fields = append(result.Fields, entry)
				continue
			}
		}
		var field FieldResult
		var err error
		if f.Kind == "option" {
			field, err = s.SelectOption(ctx, req)
		} else {
			field, err = s.SetField(ctx, req)
		}
		entry.Field = &field
		entry.Status = field.Status
		if err != nil {
			entry.Error = err.Error()
			if entry.Status == "" {
				entry.Status = "failed"
			}
			result.Failed++
			result.Fields = append(result.Fields, entry)
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if formContextFailure(err) {
				return stop(i+1, err)
			}
			// Later fields are independent inputs; continue so one pass
			// reports every problem, but never submit from here.
			continue
		}
		result.Applied++
		result.Fields = append(result.Fields, entry)
	}
	// Validation messages often appear only after every field changed.
	time.Sleep(50 * time.Millisecond)
	for i := range result.Fields {
		current, err := s.ReadField(ctx, pinned[result.Fields[i].Index])
		if err != nil {
			if formContextFailure(err) {
				return stop(len(p.Fields), err)
			}
			continue
		}
		result.Fields[i].Valid, result.Fields[i].Message = current.Valid, current.Message
		if current.Valid != nil && !*current.Valid {
			result.Invalid++
		}
	}
	switch {
	case result.Failed > 0 || result.Invalid > 0:
		result.Next = "Fix only the failed or invalid fields (fill_form with only_invalid=true), then submit once and wait for a confirmation state."
	default:
		result.Next = "All fields hold their values and pass validation. Submitting is a separate step; acceptance needs its own wait_for/assert on the confirmation."
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("%w: %d of %d fields were not applied", ErrFieldNotApplied, result.Failed, len(p.Fields))
	}
	return result, nil
}

// formContextFailure ends the batch when later writes cannot safely share
// its original context, or an earlier write has an unknown outcome.
func formContextFailure(err error) bool {
	for _, code := range []string{"stale_document", "stale_frame", "wrong_tab", "frame_unreachable", "outcome_unknown"} {
		if strings.Contains(err.Error(), code) {
			return true
		}
	}
	return false
}

// formFieldHolds reports whether a field already holds the requested value
// under the field's own comparison semantics.
func formFieldHolds(f FormField, current FieldResult) bool {
	status := CompareTypedFieldValue(current.Kind, current.Name, f.Value, current.Before, "\x00")
	if current.Kind == "select" {
		status = CompareSelectOption(f.Value, current.Before, current.AcceptedOption, "\x00")
	}
	return status == FieldApplied || status == FieldNormalized
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, r := range s {
			if r == c {
				return true
			}
		}
	}
	return false
}

// Health separates a lost browser, a closed tab, a blocked page and pending
// input recovery, so recovery can stay inside the affected context.
type Health struct {
	BrowserConnected     bool   `json:"browser_connected"`
	TabAlive             bool   `json:"tab_alive"`
	TabID                string `json:"tab_id,omitempty"`
	URL                  string `json:"url,omitempty"`
	ReadyState           string `json:"ready_state,omitempty"`
	PageResponsive       bool   `json:"page_responsive"`
	PendingDialogs       int    `json:"pending_dialogs"`
	RecentExceptions     int    `json:"recent_exceptions"`
	InputRecoveryPending bool   `json:"input_recovery_pending"`
	Diagnosis            string `json:"diagnosis"`
	Recovery             string `json:"recovery"`
}

// Health probes the session without dispatching input or navigating.
func (s *chromedpSession) Health(parent context.Context) Health {
	h := Health{TabID: s.ActiveTab()}
	s.mu.Lock()
	h.BrowserConnected = s.rootCtx != nil && s.rootCtx.Err() == nil
	h.TabAlive = s.ctx != nil && s.ctx.Err() == nil
	h.PendingDialogs = len(s.dialogs)
	for _, entry := range s.console {
		if entry.Type == "exception" && time.Since(entry.Time) < 2*time.Minute {
			h.RecentExceptions++
		}
	}
	s.mu.Unlock()
	s.inputMu.Lock()
	h.InputRecoveryPending = s.inputRecovery != nil
	s.inputMu.Unlock()
	if h.TabAlive && h.PendingDialogs == 0 {
		ctx, cancel := context.WithTimeout(s.currentContext(), 3*time.Second)
		stop := context.AfterFunc(parent, cancel)
		var state struct {
			URL   string `json:"url"`
			Ready string `json:"ready"`
		}
		err := chromedp.Run(ctx, chromedp.Evaluate(`({url: location.href, ready: document.readyState})`, &state))
		stop()
		cancel()
		h.PageResponsive = err == nil
		h.URL, h.ReadyState = redactedURL(state.URL), state.Ready
	}
	switch {
	case !h.BrowserConnected:
		h.Diagnosis, h.Recovery = "browser_disconnected", "The browser process or connection is gone. If the task is still active, call close once, then navigate to the task URL."
	case !h.TabAlive:
		h.Diagnosis, h.Recovery = "tab_closed", "The controlled tab closed. List tabs and select (or tab_return to) the intended tab; the browser itself is still running."
	case h.PendingDialogs > 0:
		h.Diagnosis, h.Recovery = "dialog_blocking", "A native dialog blocks the page. Inspect it with dialog_wait and answer with dialog_handle."
	case h.InputRecoveryPending:
		h.Diagnosis, h.Recovery = "input_cleanup_pending", "Held keys or buttons are not confirmed released. Further input waits for recovery; observe the page and do not replay the partial gesture."
	case !h.PageResponsive:
		h.Diagnosis, h.Recovery = "page_unresponsive", "The page did not answer a trivial evaluation within 3s. Wait (wait_for ready) or observe before acting."
	default:
		h.Diagnosis, h.Recovery = "healthy", "No session-level problem; a failing step is a target or page-state problem. Observe and resolve the target again."
	}
	return h
}
