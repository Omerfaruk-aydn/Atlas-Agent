package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

type taskView struct {
	TaskID       string `json:"task_id"`
	State        string `json:"state"`
	Goal         string `json:"goal"`
	Open         int    `json:"open"`
	Progress     int    `json:"progress"`
	Reobserve    string `json:"reobserve_required"`
	Requirements []struct {
		ID          string   `json:"id"`
		Text        string   `json:"text"`
		Status      string   `json:"status"`
		Effective   string   `json:"effective_status"`
		StaleReason string   `json:"stale_reason"`
		Evidence    []string `json:"evidence"`
		Revised     bool     `json:"revised"`
	} `json:"requirements"`
	History []struct {
		ID    string `json:"task_id"`
		State string `json:"state"`
		Open  int    `json:"open"`
	} `json:"history"`
	Auto struct {
		Count      int    `json:"count"`
		StopReason string `json:"stop_reason"`
	} `json:"auto_continuation"`
}

func (h *browserHarness) status() taskView {
	h.t.Helper()
	resp := h.run(`{"action":"task_status"}`)
	require.False(h.t, resp.IsError, resp.Content)
	var v taskView
	require.NoError(h.t, json.Unmarshal([]byte(resp.Content), &v), resp.Content)
	return v
}

func (h *browserHarness) requirement(id string) (status, effective, reason string) {
	h.t.Helper()
	for _, r := range h.status().Requirements {
		if r.ID == id {
			return r.Status, r.Effective, r.StaleReason
		}
	}
	h.t.Fatalf("requirement %s missing", id)
	return
}

func (h *browserHarness) verify(id string) string {
	h.t.Helper()
	op := opOf(h.t, h.run(`{"action":"read_field","advanced":{"label":"Width"}}`).Content).ID
	resp := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":%q,"status":"verified","evidence":[%q]}}`, id, op))
	require.False(h.t, resp.IsError, resp.Content)
	return op
}

func (h *browserHarness) sessionID() string { return "harness-" + h.t.Name() }

// TestBrowserTaskSecondTaskDoesNotInheritState is the regression for a new
// task in the same conversation taking over the previous task's verified
// requirement with the same id.
func TestBrowserTaskSecondTaskDoesNotInheritState(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	h.run(`{"action":"task_plan","task":{"task_id":"login","goal":"Design the login screen","requirements":[{"id":"screen","text":"Login screen exists"}]}}`)
	h.verify("screen")
	require.Zero(t, h.status().Open)

	// A different goal without saying whether it is a new task is refused,
	// and nothing changes.
	ambiguous := h.run(`{"action":"task_plan","task":{"goal":"Design the settings screen","requirements":[{"id":"screen","text":"Settings screen exists"}]}}`)
	require.True(t, ambiguous.IsError)
	require.Contains(t, ambiguous.Content, "mode=new")
	require.Equal(t, "login", h.status().TaskID)

	second := h.run(`{"action":"task_plan","task":{"mode":"new","task_id":"settings","goal":"Design the settings screen","requirements":[{"id":"screen","text":"Settings screen exists"}]}}`)
	require.False(t, second.IsError, second.Content)
	v := h.status()
	require.Equal(t, "settings", v.TaskID)
	require.Equal(t, 1, v.Open, "the new task starts open")
	require.Equal(t, "Settings screen exists", v.Requirements[0].Text)
	require.Equal(t, ReqPending, v.Requirements[0].Status)
	require.Empty(t, v.Requirements[0].Evidence)
	require.Len(t, v.History, 1)
	require.Equal(t, "login", v.History[0].ID)
	require.Equal(t, taskCompleted, v.History[0].State)

	// The new task's own continuation budget is fresh.
	_, ok := BrowserTaskContinuation(h.root, h.sessionID())
	require.True(t, ok)
	require.Equal(t, 1, h.status().Auto.Count)

	reused := h.run(`{"action":"task_plan","task":{"mode":"new","task_id":"login","requirements":[{"id":"x","text":"Again"}]}}`)
	require.True(t, reused.IsError, "an archived task id is not reused")
}

func TestBrowserTaskExtendAddsAndRevisesRequirements(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	h.run(`{"action":"task_plan","task":{"task_id":"card","goal":"Card","requirements":[{"id":"size","text":"390 wide","verify":"structural"},{"id":"color","text":"Blue fill"}]}}`)
	h.verify("size")
	h.verify("color")

	// Same id, same text: unchanged. New id: added. Same id with another
	// text or condition: revised back to pending without its old evidence.
	resp := h.run(`{"action":"task_plan","task":{"mode":"extend","requirements":[{"id":"size","text":"390 wide","verify":"structural"},{"id":"radius","text":"Radius 16"},{"id":"color","text":"Green fill"}]}}`)
	require.False(t, resp.IsError, resp.Content)
	v := h.status()
	require.Equal(t, "card", v.TaskID)
	byID := map[string]int{}
	for i, r := range v.Requirements {
		byID[r.ID] = i
	}
	require.Equal(t, ReqVerified, v.Requirements[byID["size"]].Status)
	require.Equal(t, ReqPending, v.Requirements[byID["radius"]].Status)
	color := v.Requirements[byID["color"]]
	require.Equal(t, ReqPending, color.Status)
	require.True(t, color.Revised)
	require.Empty(t, color.Evidence)
	require.Equal(t, 2, v.Open)

	// A changed verification condition is a revision too.
	h.run(`{"action":"task_plan","task":{"task_id":"card","requirements":[{"id":"size","text":"390 wide","verify":"visual"}]}}`)
	status, _, _ := h.requirement("size")
	require.Equal(t, ReqPending, status)

	wrong := h.run(`{"action":"task_update","task":{"task_id":"other","id":"size","status":"in_progress"}}`)
	require.True(t, wrong.IsError, "updates name the active task")
}

// TestBrowserTaskNewTaskWhileOldIsOpen covers the user switching to other
// work before the first task finished.
func TestBrowserTaskNewTaskWhileOldIsOpen(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	h.run(`{"action":"task_plan","task":{"task_id":"first","requirements":[{"id":"a","text":"First thing"}]}}`)
	for i := 0; i < 3; i++ {
		BrowserTaskContinuation(h.root, h.sessionID())
	}
	require.Contains(t, h.status().Auto.StopReason, "no_progress", "the reason the continuation stopped is recorded")

	h.run(`{"action":"task_plan","task":{"mode":"new","task_id":"second","requirements":[{"id":"a","text":"Second thing"}]}}`)
	v := h.status()
	require.Equal(t, taskSuperseded, v.History[0].State)
	require.Equal(t, 1, v.History[0].Open)
	require.Zero(t, v.Auto.Count)
	require.Empty(t, v.Auto.StopReason)
	prompt, ok := BrowserTaskContinuation(h.root, h.sessionID())
	require.True(t, ok, "the new task does not inherit the old task's stop")
	require.Contains(t, prompt, `"second"`)
	require.Contains(t, prompt, "Second thing")
	require.NotContains(t, prompt, "First thing")
}

// TestBrowserTaskV1CheckpointMigrates loads a checkpoint written before task
// identities: it becomes the active task, and its verified evidence from an
// earlier process must be verified again.
func TestBrowserTaskV1CheckpointMigrates(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	path := filepath.Join(h.root, browserTaskPath(h.sessionID()))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	v1 := map[string]any{
		"version": 1, "goal": "Old goal", "continuations": 7, "no_progress_continuations": 1, "progress_mark": 4001, "steps_total": 4,
		"requirements": []map[string]any{
			{"id": "done", "text": "Verified earlier", "verify": "either", "status": "verified", "evidence": []map[string]any{{"op": "b3", "action": "read_field", "verification": "observed", "epoch": "p-old", "at": time.Now()}}},
			{"id": "todo", "text": "Still open", "verify": "either", "status": "pending"},
		},
		"steps": []map[string]any{{"op": "b3", "action": "read_field", "ok": true, "epoch": "p-old", "at": time.Now()}},
	}
	data, err := json.Marshal(v1)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	v := h.status()
	require.Equal(t, "t1", v.TaskID)
	require.Equal(t, "Old goal", v.Goal)
	require.Equal(t, 2, v.Open, "stale verified evidence counts as open")
	require.NotEmpty(t, v.Reobserve)
	_, effective, reason := h.requirement("done")
	require.Equal(t, reqReverify, effective)
	require.Equal(t, "earlier_process", reason)
	prompt, ok := BrowserTaskContinuation(h.root, h.sessionID())
	require.True(t, ok)
	require.Contains(t, prompt, "verify again: earlier_process")
	require.Equal(t, 1, h.status().Auto.Count, "continuation counters are not carried over from v1")

	// An op id from the earlier process does not name an op of this one,
	// even when this process has made an op with the same sequence number.
	h.run(`{"action":"observe"}`)
	h.run(`{"action":"observe"}`)
	h.run(`{"action":"observe"}`)
	old := h.run(`{"action":"task_update","task":{"id":"done","status":"verified","evidence":["b3"]}}`)
	require.True(t, old.IsError)
	require.Contains(t, old.Content, "not a browser call of this session in this process")

	h.verify("done")
	h.verify("todo")
	_, ok = BrowserTaskContinuation(h.root, h.sessionID())
	require.False(t, ok, "re-verified in this process: complete")
}

// TestBrowserTaskStaleEvidenceAfterRestartAndChanges is the regression for
// an earlier process's evidence counting as complete, plus the other events
// that end a verification's validity.
func TestBrowserTaskStaleEvidenceAfterRestartAndChanges(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"task_plan","task":{"task_id":"t","requirements":[{"id":"w","text":"Width 390"},{"id":"p","text":"Pending part"}]}}`)
	h.verify("w")
	path := filepath.Join(h.root, browserTaskPath(h.sessionID()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Restart: the checkpoint's evidence belongs to another process. Every
	// requirement verified there is open again, next to the pending one.
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(data), browserProcessEpoch, "p-old")), 0o600))
	v := h.status()
	require.Equal(t, 2, v.Open)
	require.NotEmpty(t, v.Reobserve)
	_, ok := BrowserTaskContinuation(h.root, h.sessionID())
	require.True(t, ok, "stale verification is not completion")
	h.verify("w")
	status, effective, _ := h.requirement("w")
	require.Equal(t, ReqVerified, status)
	require.Empty(t, effective, "verified again in this process")

	// The browser reconnected: evidence from the old connection is stale.
	sess.instance = "browser-2"
	h.run(`{"action":"observe"}`)
	_, effective, reason := h.requirement("w")
	require.Equal(t, reqReverify, effective)
	require.Equal(t, "browser_reconnected", reason)
	h.verify("w")

	// The same URL loaded a new document: the old document's evidence is stale.
	sess.doc = "doc-reloaded"
	h.run(`{"action":"observe"}`)
	_, effective, reason = h.requirement("w")
	require.Equal(t, reqReverify, effective)
	require.Equal(t, "document_changed", reason)
	h.verify("w")

	// The verified target is changed again: that verification is outdated,
	// and evidence from before the change is refused.
	setOp := opOf(t, h.run(`{"action":"set_field","advanced":{"label":"Width","value":"390"}}`).Content).ID
	h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"w","status":"verified","evidence":[%q]}}`, setOp))
	h.run(`{"action":"set_field","advanced":{"label":"Width","value":"400"}}`)
	_, effective, reason = h.requirement("w")
	require.Equal(t, reqReverify, effective)
	require.Equal(t, "target_changed_after_verification", reason)
	outdated := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"w","status":"verified","evidence":[%q]}}`, setOp))
	require.True(t, outdated.IsError)
	require.Contains(t, outdated.Content, "changed its target again")

	// An unrelated change keeps it.
	h.verify("w")
	h.run(`{"action":"set_field","advanced":{"label":"Height","value":"844"}}`)
	_, effective, _ = h.requirement("w")
	require.Empty(t, effective)
}

// TestBrowserTaskBlockersNeedEvidenceAndExpire covers blocked requirements:
// page blockers need the op that showed them and are re-checked after a
// restart; conversation blockers need a note.
func TestBrowserTaskBlockersNeedEvidenceAndExpire(t *testing.T) {
	t.Parallel()
	h := newBrowserHarness(t, newObservingSession())
	h.run(`{"action":"task_plan","task":{"task_id":"t","requirements":[{"id":"save","text":"Saved"},{"id":"pay","text":"Upgrade decided"}]}}`)
	bare := h.run(`{"action":"task_update","task":{"id":"save","status":"blocked","blocker":"account_or_plan_limit"}}`)
	require.True(t, bare.IsError)
	obs := opOf(t, h.run(`{"action":"observe"}`).Content).ID
	require.False(t, h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"save","status":"blocked","blocker":"account_or_plan_limit","evidence":[%q]}}`, obs)).IsError)
	require.True(t, h.run(`{"action":"task_update","task":{"id":"pay","status":"blocked","blocker":"needs_user_approval"}}`).IsError)
	require.False(t, h.run(`{"action":"task_update","task":{"id":"pay","status":"blocked","blocker":"needs_user_approval","note":"The user decides whether to upgrade"}}`).IsError)
	require.Zero(t, h.status().Open)
	_, ok := BrowserTaskContinuation(h.root, h.sessionID())
	require.False(t, ok)

	path := filepath.Join(h.root, browserTaskPath(h.sessionID()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.ReplaceAll(string(data), browserProcessEpoch, "p-old")), 0o600))
	_, effective, _ := h.requirement("save")
	require.Equal(t, reqRecheck, effective, "a page blocker seen by an earlier process is checked again")
	_, effective, _ = h.requirement("pay")
	require.Empty(t, effective, "the user's own decision stays recorded")
	require.Equal(t, 1, h.status().Open)
}

// TestBrowserTaskProgressIsRealProgress checks that reads, repeats and
// failures do not keep automatic continuation going, while distinct
// successful changes do.
func TestBrowserTaskProgressIsRealProgress(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*browserHarness, *observingSession, func() bool) {
		sess := newObservingSession()
		h := newBrowserHarness(t, sess)
		h.run(`{"action":"task_plan","task":{"task_id":"t","requirements":[{"id":"a","text":"Thing"}]}}`)
		return h, sess, func() bool {
			_, ok := BrowserTaskContinuation(h.root, h.sessionID())
			return ok
		}
	}
	t.Run("reads", func(t *testing.T) {
		t.Parallel()
		h, _, cont := setup(t)
		require.True(t, cont())
		for i := 0; i < 3; i++ {
			h.run(`{"action":"observe"}`)
			h.run(`{"action":"health"}`)
		}
		require.True(t, cont(), "one continuation without progress is allowed")
		require.False(t, cont(), "the second in a row stops")
		require.Contains(t, h.status().Auto.StopReason, "no_progress")
		require.Zero(t, h.status().Progress)
	})
	t.Run("failures", func(t *testing.T) {
		t.Parallel()
		h, sess, cont := setup(t)
		sess.fieldErr = fmt.Errorf("value_not_applied: still old")
		require.True(t, cont())
		for i := 0; i < 4; i++ {
			h.run(fmt.Sprintf(`{"action":"set_field","advanced":{"label":"Width","value":"%d"}}`, 300+i))
		}
		require.True(t, cont())
		require.False(t, cont(), "failed changes are not progress")
		require.Zero(t, h.status().Progress)
	})
	t.Run("changes", func(t *testing.T) {
		t.Parallel()
		h, _, cont := setup(t)
		require.True(t, cont())
		for i := 0; i < 4; i++ {
			h.run(fmt.Sprintf(`{"action":"set_field","advanced":{"label":"Width","value":"%d"}}`, 300+i))
			require.True(t, cont(), "a new successful change is progress")
		}
		require.Equal(t, 4, h.status().Progress)
		// The identical successful call again is not new progress.
		h.run(`{"action":"set_field","advanced":{"label":"Width","value":"303"}}`)
		require.Equal(t, 4, h.status().Progress)
		require.True(t, cont())
		require.False(t, cont())
	})
}

// TestBrowserTaskNavigationIsNotProgress replays the pattern of session
// aaae8f0a, where 228 "progress" points came largely from escape, tool
// shortcuts and zoom chords: moving around a design is not delivering it.
func TestBrowserTaskNavigationIsNotProgress(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"task_plan","task":{"task_id":"t","requirements":[{"id":"a","text":"Thing"}]}}`)
	cont := func() bool {
		_, ok := BrowserTaskContinuation(h.root, h.sessionID())
		return ok
	}
	require.True(t, cont())
	for _, key := range []string{"escape", "shift+2", "f", "t", "r", "arrowdown"} {
		h.run(fmt.Sprintf(`{"action":"key","key":%q}`, key))
		h.run(`{"action":"screenshot"}`)
	}
	require.Zero(t, h.status().Progress)
	require.True(t, cont(), "one continuation without progress is allowed")
	require.False(t, cont(), "navigation alone does not keep the task going")
	require.Contains(t, h.status().Auto.StopReason, "no_progress")

	require.True(t, browserDeliveryAction("set_field", BrowserParams{}))
	require.True(t, browserDeliveryAction("insert_text", BrowserParams{}))
	require.True(t, browserDeliveryAction("key", BrowserParams{Key: "delete"}))
	require.True(t, browserDeliveryAction("key_combo", BrowserParams{Advanced: browserRequest("k", "ctrl", "alt")}))
	require.False(t, browserDeliveryAction("key_combo", BrowserParams{Advanced: browserRequest("2", "shift")}))
}

func browserRequest(key string, modifiers ...string) browser.Request {
	return browser.Request{Key: key, Modifiers: modifiers}
}

// TestBrowserTaskExpectRefusesUnrelatedEvidence: a requirement such as
// "real components" or "Auto Layout" cannot be closed with any observation
// at all; the cited evidence must show the property's own marker text.
// Without it the task stays open and the completion gate keeps working.
func TestBrowserTaskExpectRefusesUnrelatedEvidence(t *testing.T) {
	t.Parallel()
	sess := newObservingSession()
	h := newBrowserHarness(t, sess)
	h.run(`{"action":"task_plan","task":{"task_id":"t","requirements":[{"id":"components","text":"Button is a real component","verify":"structural","expect":"Main component"}]}}`)
	op := opOf(t, h.run(`{"action":"observe"}`).Content).ID
	resp := h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"components","status":"verified","evidence":[%q]}}`, op))
	require.True(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `expects evidence showing "Main component"`)
	_, cont := BrowserTaskContinuation(h.root, h.sessionID())
	require.True(t, cont, "the requirement stays open")

	sess.elements = append(sess.elements, browser.SnapshotElement{Ref: "e9", Role: "heading", Tag: "h3", Name: "Main component", Context: `region "Right sidebar"`, Visible: true})
	op = opOf(t, h.run(`{"action":"observe","advanced":{"mode":"full"}}`).Content).ID
	resp = h.run(fmt.Sprintf(`{"action":"task_update","task":{"id":"components","status":"verified","evidence":[%q]}}`, op))
	require.False(t, resp.IsError, resp.Content)
	require.Zero(t, h.status().Open)
}
