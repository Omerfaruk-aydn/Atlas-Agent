package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Requirement statuses of a browser completion contract.
const (
	ReqPending    = "pending"
	ReqInProgress = "in_progress"
	ReqVerified   = "verified"
	ReqBlocked    = "blocked"
	// Effective statuses: a verified requirement whose evidence no longer
	// describes the current page must be verified again, and a blocker seen
	// by an earlier process must be checked again. Both count as open.
	reqReverify = "reverify"
	reqRecheck  = "recheck_blocker"
)

// Task states. Only the active task is continued; starting a new task
// archives the previous one as completed or superseded.
const (
	taskActive     = "active"
	taskCompleted  = "completed"
	taskSuperseded = "superseded"
)

// BrowserContinuationMarker starts every automatic continuation prompt, so
// the agent can tell a continuation turn from a turn the user started.
const BrowserContinuationMarker = "[Atlas completion check]"

// browserBlockers are the concrete reasons a requirement may stop on.
// Effort, length and context size are deliberately absent: they are not
// blockers, and a contract cannot record them as one.
var browserBlockers = []string{
	"needs_user_input", "needs_user_approval", "permission_denied", "account_or_plan_limit",
	"payment_required", "captcha_or_2fa", "site_error", "tool_unavailable", "user_canceled", "out_of_scope",
}

// pageBlockers are blockers the page shows; they need evidence (the op that
// observed or hit them) and are checked again after a restart. The others
// come from the conversation and need a note saying what the user must do.
var pageBlockers = map[string]bool{
	"permission_denied": true, "account_or_plan_limit": true, "payment_required": true,
	"captcha_or_2fa": true, "site_error": true, "tool_unavailable": true,
}

// BrowserTaskParams carries completion-contract updates.
type BrowserTaskParams struct {
	TaskID       string                    `json:"task_id,omitempty" description:"Contract identity. For task_plan: a short id for this user task (e.g. \"landing-hero\"). A different id than the active task's starts a new task; the same id extends it. For task_update: optional check that the update targets the active task."`
	Mode         string                    `json:"mode,omitempty" description:"For task_plan: new (start a separate task; the previous one is archived and nothing carries over) or extend (add or revise requirements of the active task). Required when another task is active and no task_id decides it."`
	Goal         string                    `json:"goal,omitempty" description:"For task_plan: the user's requested outcome in one sentence."`
	Requirements []BrowserRequirementInput `json:"requirements,omitempty" description:"For task_plan: 1-40 checkable requirements. With extend, new ids are added and an existing id with changed text or verify is revised back to pending (its old evidence no longer counts)."`
	ID           string                    `json:"id,omitempty" description:"For task_update: requirement id."`
	Status       string                    `json:"status,omitempty" description:"For task_update: pending, in_progress, verified or blocked."`
	Evidence     []string                  `json:"evidence,omitempty" description:"For task_update: op_id values from this process. verified needs successful verifying calls (assert, wait_for, set_field, read_field, observe, screenshot) made after the last change to their target; blocked on a page blocker needs the op that showed it."`
	Note         string                    `json:"note,omitempty" description:"Short non-secret note. Required for blockers that come from the conversation (needs_user_input, needs_user_approval, user_canceled, out_of_scope): say what the user must do or decided."`
	Blocker      string                    `json:"blocker,omitempty" description:"For status=blocked: needs_user_input, needs_user_approval, permission_denied, account_or_plan_limit, payment_required, captcha_or_2fa, site_error, tool_unavailable, user_canceled or out_of_scope."`
}

// BrowserRequirementInput declares one requirement.
type BrowserRequirementInput struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Verify  string `json:"verify,omitempty" description:"structural (current DOM/value evidence), visual (current screenshot), either (default), observation (historical source-scoped research) or delivery (saved final response only)."`
	Expect  string `json:"expect,omitempty" description:"Optional text a structural evidence call must have returned, e.g. a panel label that exists only when the property is real (\"Main component\", \"Remove auto layout\", \"Navigate to\"). Evidence without it is refused."`
	Subject string `json:"subject,omitempty" description:"For observation: explicit entity whose claim is being checked; it must occur in the source's returned text."`
	Source  string `json:"source,omitempty" description:"For observation: exact HTTP(S) source URL returned by the evidence call. Observation requires source, subject and expect; delivery requires expect text in the final response."`
}

// browserEvidence ties a verification to where it was made: the process,
// browser connection, tab, top document and target. It stops counting when
// any of these changes or the target is changed afterwards.
type browserEvidence struct {
	Op           string    `json:"op"`
	Seq          int       `json:"seq,omitempty"`
	Action       string    `json:"action"`
	Verification string    `json:"verification"`
	Epoch        string    `json:"epoch"`
	Browser      string    `json:"browser,omitempty"`
	Tab          string    `json:"tab,omitempty"`
	Document     string    `json:"document,omitempty"`
	Target       string    `json:"target,omitempty"`
	TargetKeys   []string  `json:"target_keys,omitempty"`
	Stale        string    `json:"stale,omitempty"`
	At           time.Time `json:"at"`
	Kind         string    `json:"kind,omitempty"`
	Source       string    `json:"source,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	Claim        string    `json:"claim,omitempty"`
	ContentHash  string    `json:"content_hash,omitempty"`
}

type browserRequirement struct {
	ID              string            `json:"id"`
	Text            string            `json:"text"`
	Verify          string            `json:"verify"`
	Expect          string            `json:"expect,omitempty"`
	Subject         string            `json:"subject,omitempty"`
	Source          string            `json:"source,omitempty"`
	Status          string            `json:"status"`
	Note            string            `json:"note,omitempty"`
	Blocker         string            `json:"blocker,omitempty"`
	BlockerEvidence []string          `json:"blocker_evidence,omitempty"`
	BlockedEpoch    string            `json:"blocked_epoch,omitempty"`
	Evidence        []browserEvidence `json:"evidence,omitempty"`
	Revisions       int               `json:"revisions,omitempty"`
	Updated         time.Time         `json:"updated"`
}

// browserStep is a checkpoint entry: what was done, never with typed
// values, cookies, tokens, refs or coordinates.
type browserStep struct {
	Op           string    `json:"op"`
	Action       string    `json:"action"`
	Target       string    `json:"target,omitempty"`
	OK           bool      `json:"ok"`
	Verification string    `json:"verification,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Epoch        string    `json:"epoch"`
	At           time.Time `json:"at"`
}

type browserTask struct {
	ID           string               `json:"task_id"`
	State        string               `json:"state"`
	Goal         string               `json:"goal"`
	Requirements []browserRequirement `json:"requirements"`
	Steps        []browserStep        `json:"steps"`
	Created      time.Time            `json:"created"`
	Updated      time.Time            `json:"updated"`
	StepsTotal   int                  `json:"steps_total"`
	// Progress counts distinct successful delivery changes; repeating the
	// same call, reads, failures, navigation keys, tool shortcuts, zoom and
	// tab changes do not count.
	Progress     int      `json:"progress"`
	ProgressKeys []string `json:"progress_keys,omitempty"`
	// Documents is the latest top document seen per tab, Browser the latest
	// browser connection; evidence from others is stale.
	Documents              map[string]string `json:"documents,omitempty"`
	Browser                string            `json:"browser,omitempty"`
	Continuations          int               `json:"continuations"`
	NoProgressContinuation int               `json:"no_progress_continuations"`
	ProgressSignature      string            `json:"progress_signature,omitempty"`
	StopReason             string            `json:"stop_reason,omitempty"`
	Migrated               bool              `json:"migrated_from_v1,omitempty"`
}

type browserTaskRecord struct {
	ID       string    `json:"task_id"`
	Goal     string    `json:"goal"`
	State    string    `json:"state"`
	Open     int       `json:"open"`
	Verified int       `json:"verified"`
	Blocked  int       `json:"blocked"`
	Closed   time.Time `json:"closed"`
}

// browserTaskFile is the per-session checkpoint: the one active task and a
// short history of earlier tasks of the same conversation.
type browserTaskFile struct {
	Version int                 `json:"version"`
	Seq     int                 `json:"seq"`
	Active  *browserTask        `json:"active,omitempty"`
	History []browserTaskRecord `json:"history,omitempty"`
}

// browserTaskV1 is the checkpoint format before task identities: one task
// per conversation, kept for loading old checkpoints.
type browserTaskV1 struct {
	Goal         string               `json:"goal"`
	Requirements []browserRequirement `json:"requirements"`
	Steps        []browserStep        `json:"steps"`
	Created      time.Time            `json:"created"`
	StepsTotal   int                  `json:"steps_total"`
}

const (
	browserTaskVersion = 2
	// maxAutoContinuations bounds automatic continuation of one task; two
	// continuations in a row without new progress stop it.
	maxAutoContinuations      = 12
	maxNoProgressContinuation = 2
	maxCheckpointSteps        = 300
	maxProgressKeys           = 500
	maxTaskHistory            = 10
)

var browserTaskMu sync.Mutex

func browserTaskPath(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(".atlas", "browser", "tasks", fmt.Sprintf("%x.json", sum[:16]))
}

func loadBrowserTasks(root, sessionID string) (*browserTaskFile, error) {
	if root == "" {
		return nil, nil
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	f, err := fs.Open(browserTaskPath(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 2*1024*1024 {
		return nil, errors.New("browser task checkpoint exceeds size limit")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("browser task checkpoint is unreadable: %w", err)
	}
	if _, v2 := probe["active"]; v2 || probe["history"] != nil || probe["seq"] != nil {
		var file browserTaskFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("browser task checkpoint is unreadable: %w", err)
		}
		return &file, nil
	}
	// A v1 checkpoint becomes the active task. Its evidence and blockers
	// come from an earlier process, so they are re-verified, never trusted;
	// its continuation counters are not carried over.
	var old browserTaskV1
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("browser task checkpoint is unreadable: %w", err)
	}
	task := &browserTask{ID: "t1", State: taskActive, Goal: old.Goal, Requirements: old.Requirements, Steps: old.Steps, Created: old.Created, StepsTotal: old.StepsTotal, Migrated: true}
	return &browserTaskFile{Version: browserTaskVersion, Seq: 1, Active: task}, nil
}

func saveBrowserTasks(root, sessionID string, file *browserTaskFile) error {
	if root == "" {
		return nil
	}
	file.Version = browserTaskVersion
	if file.Active != nil {
		file.Active.Updated = time.Now()
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	fs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer fs.Close()
	path := browserTaskPath(sessionID)
	if err := fs.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := fs.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return fs.Rename(tmp, path)
}

// evidenceStale names why one piece of evidence no longer describes the
// current state, or "" while it does.
func (t *browserTask) evidenceStale(e browserEvidence) string {
	if e.Kind == "observation" && validResearchEvidence(e) {
		return ""
	}
	if e.Kind == "delivery" && e.Op != "" && e.ContentHash != "" {
		return ""
	}
	switch {
	case e.Stale != "":
		return e.Stale
	case e.Epoch != browserProcessEpoch:
		return "earlier_process"
	case e.Browser != "" && t.Browser != "" && e.Browser != t.Browser:
		return "browser_reconnected"
	case e.Document != "" && t.Documents[e.Tab] != "" && t.Documents[e.Tab] != e.Document:
		return "document_changed"
	}
	return ""
}

// effective is the status completion is decided on, with its reason.
func (t *browserTask) effective(r browserRequirement) (string, string) {
	switch r.Status {
	case ReqVerified:
		if r.Verify == "observation" || r.Verify == "delivery" {
			if len(r.Evidence) == 0 {
				return reqReverify, "missing_provenance"
			}
			for _, e := range r.Evidence {
				if !requirementEvidenceMatches(r, e) {
					return reqReverify, "missing_provenance"
				}
			}
		}
		for _, e := range r.Evidence {
			if reason := t.evidenceStale(e); reason != "" {
				return reqReverify, reason
			}
		}
	case ReqBlocked:
		if pageBlockers[r.Blocker] && r.BlockedEpoch != browserProcessEpoch {
			return reqRecheck, "earlier_process"
		}
	}
	return r.Status, ""
}

func (t *browserTask) counts() (open, verified, blocked int) {
	for _, r := range t.Requirements {
		switch status, _ := t.effective(r); status {
		case ReqVerified:
			verified++
		case ReqBlocked:
			blocked++
		default:
			open++
		}
	}
	return open, verified, blocked
}

func progressKey(op BrowserOp) string {
	sum := sha256.Sum256([]byte(op.Key + "\x00" + op.Document))
	return hex.EncodeToString(sum[:8])
}

// recordBrowserStep applies a finished call to the active task's checkpoint:
// it appends the step, tracks the browser connection and documents, marks
// evidence stale when its target was changed again, and counts progress.
// Without an active task nothing is written.
func recordBrowserStep(root, sessionID string, op BrowserOp) {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return
	}
	task := file.Active
	task.Steps = append(task.Steps, browserStep{Op: op.ID, Action: op.Action, Target: truncateRunes(op.Target, 80), OK: op.OK, Verification: op.Verification, ErrorCode: op.ErrorCode, Epoch: op.Epoch, At: op.At})
	task.StepsTotal++
	if len(task.Steps) > maxCheckpointSteps {
		task.Steps = task.Steps[len(task.Steps)-maxCheckpointSteps:]
	}
	if op.Browser != "" {
		task.Browser = op.Browser
	}
	if op.Tab != "" && op.Document != "" {
		if task.Documents == nil {
			task.Documents = map[string]string{}
		}
		if _, known := task.Documents[op.Tab]; known || len(task.Documents) < 32 {
			task.Documents[op.Tab] = op.Document
		}
	}
	changed := op.OK && browserMutating(op.Action) && op.Dispatch == "sent" && op.Verification != "failed"
	if changed || len(op.ChangedKeys) > 0 || op.ChangeUncertain {
		// Evidence about a target stops counting once that target is changed
		// again; an undo changes the last edited field, whatever its name.
		for i := range task.Requirements {
			if task.Requirements[i].Verify == "observation" || task.Requirements[i].Verify == "delivery" {
				continue
			}
			for j := range task.Requirements[i].Evidence {
				e := &task.Requirements[i].Evidence[j]
				if e.Stale != "" || e.Epoch != op.Epoch || e.Seq >= op.Seq {
					continue
				}
				if e.Tab != op.Tab {
					if op.ChangeUncertain || resolvedEvidenceChanged(op.ChangedKeys, e.TargetKeys) {
						e.Stale = "target_changed_after_verification"
					}
					continue
				}
				if browserOpInvalidates(op, e.Action, e.Target, e.TargetKeys) {
					e.Stale = "target_changed_after_verification"
				}
				if op.Action == "undo" && (e.Action == "set_field" || e.Action == "select_option" || e.Action == "fill_form" || e.Action == "read_field") {
					e.Stale = "undone_after_verification"
				}
			}
		}
	}
	if op.Delivery {
		key := progressKey(op)
		if !slices.Contains(task.ProgressKeys, key) {
			task.ProgressKeys = append(task.ProgressKeys, key)
			if len(task.ProgressKeys) > maxProgressKeys {
				task.ProgressKeys = task.ProgressKeys[len(task.ProgressKeys)-maxProgressKeys:]
			}
			task.Progress++
		}
	}
	_ = saveBrowserTasks(root, sessionID, file)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func evidenceMatches(verify, verification string) bool {
	switch verify {
	case "visual":
		return verification == "visual"
	case "structural":
		return verification == "structural" || verification == "observed"
	case "observation":
		return verification == "observed"
	case "delivery":
		return false
	}
	return verification == "visual" || verification == "structural" || verification == "observed"
}

func validRequirements(reqs []BrowserRequirementInput) error {
	if len(reqs) == 0 || len(reqs) > 40 {
		return errors.New("task_plan requires 1-40 requirements")
	}
	seen := map[string]bool{}
	for _, r := range reqs {
		if r.ID == "" || len(r.ID) > 40 || strings.TrimSpace(r.Text) == "" {
			return errors.New("each requirement needs an id (max 40 chars) and text")
		}
		if seen[r.ID] {
			return fmt.Errorf("requirement id %q appears twice", r.ID)
		}
		seen[r.ID] = true
		switch r.Verify {
		case "", "either", "structural", "visual", "observation", "delivery":
		default:
			return errors.New("verify must be structural, visual, either, observation or delivery")
		}
		if r.Verify == "observation" && (strings.TrimSpace(r.Subject) == "" || !validResearchSource(r.Source) || strings.TrimSpace(r.Expect) == "") {
			return errors.New("observation requires an explicit subject, an HTTP(S) source URL and an expected extracted claim")
		}
		if r.Verify == "delivery" && strings.TrimSpace(r.Expect) == "" {
			return errors.New("delivery requires expect text that the final response must contain")
		}
	}
	return nil
}

func newBrowserTask(file *browserTaskFile, id, goal string) (*browserTask, error) {
	file.Seq++
	if id == "" {
		id = fmt.Sprintf("t%d", file.Seq)
	}
	if len(id) > 60 {
		return nil, errors.New("task_id is at most 60 characters")
	}
	for _, h := range file.History {
		if h.ID == id {
			return nil, fmt.Errorf("task_id %q was used by an earlier task of this conversation; choose a new id", id)
		}
	}
	return &browserTask{ID: id, State: taskActive, Goal: truncateRunes(goal, 400), Created: time.Now()}, nil
}

// archive closes the active task: completed when nothing is open,
// superseded when a new task replaced unfinished work.
func (file *browserTaskFile) archive() {
	task := file.Active
	if task == nil {
		return
	}
	open, verified, blocked := task.counts()
	task.State = taskCompleted
	if open > 0 {
		task.State = taskSuperseded
	}
	file.History = append(file.History, browserTaskRecord{ID: task.ID, Goal: task.Goal, State: task.State, Open: open, Verified: verified, Blocked: blocked, Closed: time.Now()})
	if len(file.History) > maxTaskHistory {
		file.History = file.History[len(file.History)-maxTaskHistory:]
	}
	file.Active = nil
}

// planTask applies task_plan: it decides whether the request starts a new
// task or extends the active one. The decision is explicit (mode or
// task_id); a goal that merely reads differently is not taken as a new
// task, and an ambiguous request is refused rather than guessed.
func planTask(file *browserTaskFile, p BrowserTaskParams) error {
	if err := validRequirements(p.Requirements); err != nil {
		return err
	}
	active := file.Active
	start := false
	switch {
	case p.Mode != "" && p.Mode != "new" && p.Mode != "extend":
		return errors.New("mode must be new or extend")
	case active == nil:
		if p.Mode == "extend" {
			return errors.New("mode=extend needs an active task; start one with mode=new")
		}
		start = true
	case p.Mode == "new":
		if p.TaskID != "" && p.TaskID == active.ID {
			return fmt.Errorf("task_id %q is the active task; give the new task its own id, or use mode=extend to add to it", p.TaskID)
		}
		start = true
	case p.Mode == "extend":
		if p.TaskID != "" && p.TaskID != active.ID {
			return fmt.Errorf("mode=extend applies to the active task %q, not %q", active.ID, p.TaskID)
		}
	case p.TaskID != "":
		start = p.TaskID != active.ID
	case p.Goal != "" && p.Goal != active.Goal:
		open, _, _ := active.counts()
		return fmt.Errorf("task_plan is ambiguous: task %q (goal %q, %d open) is active. Pass mode=new with a new task_id when the user asked for a separate task (nothing carries over), or mode=extend to add these requirements to the active task", active.ID, active.Goal, open)
	}
	if start {
		file.archive()
		task, err := newBrowserTask(file, p.TaskID, p.Goal)
		if err != nil {
			return err
		}
		file.Active = task
	} else if p.Goal != "" {
		file.Active.Goal = truncateRunes(p.Goal, 400)
	}
	task := file.Active
	added := false
	for _, r := range p.Requirements {
		if r.Verify == "" {
			r.Verify = "either"
		}
		text := truncateRunes(r.Text, 300)
		expect := truncateRunes(strings.TrimSpace(r.Expect), 120)
		subject := truncateRunes(strings.TrimSpace(r.Subject), 160)
		source := strings.TrimSpace(r.Source)
		i := slices.IndexFunc(task.Requirements, func(e browserRequirement) bool { return e.ID == r.ID })
		if i < 0 {
			task.Requirements = append(task.Requirements, browserRequirement{ID: r.ID, Text: text, Verify: r.Verify, Expect: expect, Subject: subject, Source: source, Status: ReqPending, Updated: time.Now()})
			added = true
			continue
		}
		req := &task.Requirements[i]
		if req.Text == text && req.Verify == r.Verify && req.Expect == expect && req.Subject == subject && req.Source == source {
			continue
		}
		// A revised requirement is a new claim: what verified the old text or
		// condition does not verify this one.
		*req = browserRequirement{ID: r.ID, Text: text, Verify: r.Verify, Expect: expect, Subject: subject, Source: source, Status: ReqPending, Note: "revised; earlier evidence no longer applies", Revisions: req.Revisions + 1, Updated: time.Now()}
		added = true
	}
	if len(task.Requirements) > 40 {
		return errors.New("a contract holds at most 40 requirements")
	}
	if added {
		// New work restarts the progress bound, not the overall bound.
		task.NoProgressContinuation, task.StopReason = 0, ""
	}
	return nil
}

// updateTask applies task_update to the active task.
func updateTask(task *browserTask, stateID string, p BrowserTaskParams) error {
	if p.TaskID != "" && p.TaskID != task.ID {
		return fmt.Errorf("task %q is not the active task (active: %q); updates apply only to the active task", p.TaskID, task.ID)
	}
	i := slices.IndexFunc(task.Requirements, func(e browserRequirement) bool { return e.ID == p.ID })
	if i < 0 {
		return fmt.Errorf("unknown requirement %q in task %q", p.ID, task.ID)
	}
	req := &task.Requirements[i]
	ledger := ledgerFor(stateID)
	switch p.Status {
	case ReqPending, ReqInProgress:
		req.Blocker, req.BlockerEvidence, req.BlockedEpoch = "", nil, ""
	case ReqVerified:
		if req.Verify == "delivery" {
			return errors.New("delivery can only be verified by the saved final response; browser observations and model notes are not delivery")
		}
		if len(p.Evidence) == 0 {
			return errors.New("verified requires evidence: op_id values of successful verifying calls")
		}
		var evidence []browserEvidence
		expectSeen := false
		for _, id := range p.Evidence {
			op, ok := ledger.findOp(id)
			if !ok {
				return fmt.Errorf("evidence %s is not a browser call of this session in this process; verify again", id)
			}
			if !op.OK || !evidenceMatches(req.Verify, op.Verification) {
				return fmt.Errorf("evidence %s (%s, verification=%s) does not establish a %s requirement", id, op.Action, op.Verification, req.Verify)
			}
			if req.Verify == "observation" {
				e, err := researchEvidence(*req, op)
				if err != nil {
					return err
				}
				evidence = append(evidence, e)
				expectSeen = true
				continue
			}
			e := browserEvidence{Op: op.ID, Seq: op.Seq, Action: op.Action, Verification: op.Verification, Epoch: op.Epoch, Browser: op.Browser, Tab: op.Tab, Document: op.Document, Target: truncateRunes(op.Target, 80), TargetKeys: op.TargetKeys, At: op.At}
			if reason := task.evidenceStale(e); reason != "" {
				return fmt.Errorf("evidence %s no longer describes the page (%s); verify again", id, reason)
			}
			if later, ok := ledger.changedAfter(op); ok {
				return fmt.Errorf("evidence %s was made before %s changed its target again; verify again", id, later)
			}
			evidence = append(evidence, e)
			if req.Expect != "" && strings.Contains(op.Observed, strings.ToLower(req.Expect)) {
				expectSeen = true
			}
		}
		if req.Expect != "" && !expectSeen {
			return fmt.Errorf("requirement %s expects evidence showing %q, and none of %s returned it. Observe or assert the place where it appears (for example the selected layer's properties panel) and cite that op", req.ID, req.Expect, strings.Join(p.Evidence, ", "))
		}
		req.Evidence, req.Blocker, req.BlockerEvidence, req.BlockedEpoch = evidence, "", nil, ""
	case ReqBlocked:
		if !slices.Contains(browserBlockers, p.Blocker) {
			return fmt.Errorf("blocked requires a concrete blocker (%s). Length, number of steps or context use are not blockers: continue the work", strings.Join(browserBlockers, ", "))
		}
		if pageBlockers[p.Blocker] {
			// A page blocker is something a call saw: the dialog, the error,
			// the denied request. The op that showed it is the evidence.
			if len(p.Evidence) == 0 {
				return fmt.Errorf("blocker %s needs evidence: the op_id of the call that showed it (observe, screenshot, or the failed call)", p.Blocker)
			}
			for _, id := range p.Evidence {
				if _, ok := ledger.findOp(id); !ok {
					return fmt.Errorf("blocker evidence %s is not a browser call of this session in this process", id)
				}
			}
		} else if strings.TrimSpace(p.Note) == "" {
			return fmt.Errorf("blocker %s needs a note saying what the user must provide or decided", p.Blocker)
		}
		req.Blocker, req.BlockerEvidence, req.BlockedEpoch = p.Blocker, p.Evidence, browserProcessEpoch
	default:
		return errors.New("status must be pending, in_progress, verified or blocked")
	}
	req.Status = p.Status
	if p.Status != ReqVerified {
		req.Evidence = nil
	}
	req.Note = truncateRunes(p.Note, 300)
	req.Updated = time.Now()
	return nil
}

// runBrowserTask handles task_plan, task_update and task_status.
func runBrowserTask(root, sessionID, stateID, action string, p BrowserTaskParams) (string, error) {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil {
		return "", err
	}
	if file == nil {
		file = &browserTaskFile{Version: browserTaskVersion}
	}
	switch action {
	case "task_plan":
		if err := planTask(file, p); err != nil {
			return "", err
		}
	case "task_update":
		if file.Active == nil {
			return "", errors.New("no active browser task contract; create one with task_plan")
		}
		if err := updateTask(file.Active, stateID, p); err != nil {
			return "", err
		}
	case "task_status":
		if file.Active == nil {
			out := map[string]any{"contract": false, "next": "No active browser task contract. For multi-part deliverables call task_plan with checkable requirements first."}
			if len(file.History) > 0 {
				out["history"] = file.History
			}
			data, _ := json.Marshal(out)
			return string(data), nil
		}
	}
	if action != "task_status" {
		if err := saveBrowserTasks(root, sessionID, file); err != nil {
			return "", err
		}
	}
	return browserTaskSummary(file), nil
}

func browserTaskSummary(file *browserTaskFile) string {
	task := file.Active
	type reqView struct {
		ID          string   `json:"id"`
		Text        string   `json:"text"`
		Status      string   `json:"status"`
		Effective   string   `json:"effective_status,omitempty"`
		StaleReason string   `json:"stale_reason,omitempty"`
		Verify      string   `json:"verify"`
		Expect      string   `json:"expect,omitempty"`
		Subject     string   `json:"subject,omitempty"`
		Source      string   `json:"source,omitempty"`
		Blocker     string   `json:"blocker,omitempty"`
		Evidence    []string `json:"evidence,omitempty"`
		Revised     bool     `json:"revised,omitempty"`
	}
	counts := map[string]int{}
	var reqs []reqView
	reobserve := task.Migrated
	for _, r := range task.Requirements {
		status, reason := task.effective(r)
		counts[status]++
		v := reqView{ID: r.ID, Text: r.Text, Status: r.Status, Verify: r.Verify, Expect: r.Expect, Subject: r.Subject, Source: r.Source, Blocker: r.Blocker, Revised: r.Revisions > 0}
		if status != r.Status {
			v.Effective, v.StaleReason = status, reason
			if reason == "earlier_process" || reason == "browser_reconnected" {
				reobserve = true
			}
		}
		for _, e := range r.Evidence {
			v.Evidence = append(v.Evidence, e.Op)
		}
		reqs = append(reqs, v)
	}
	for _, s := range task.Steps {
		if s.Epoch != browserProcessEpoch {
			reobserve = true
			break
		}
	}
	open, _, _ := task.counts()
	work, delivery := task.actionableCounts()
	next := "All requirements are verified or blocked with a concrete reason; report the result with the evidence."
	if open > 0 {
		next = fmt.Sprintf("%d requirement(s) remain open. Continue working on them; finishing the turn now leaves the task incomplete.", open)
		if work == 0 && delivery > 0 {
			next = fmt.Sprintf("Browser work is ready. %d delivery requirement(s) await the final response; compose the requested output now. Only the saved final response can verify delivery.", delivery)
		}
	}
	out := map[string]any{
		"contract": true, "task_id": task.ID, "state": task.State, "goal": task.Goal, "requirements": reqs, "counts": counts, "open": open, "steps_recorded": task.StepsTotal, "progress": task.Progress, "next": next,
		"actionable_open": work, "delivery_pending": delivery,
		"auto_continuation": map[string]any{"count": task.Continuations, "limit": maxAutoContinuations, "stop_reason": task.StopReason},
	}
	if len(file.History) > 0 {
		out["history"] = file.History
	}
	if reobserve {
		out["resumed_from_checkpoint"] = true
		out["reobserve_required"] = "Steps or evidence come from an earlier process or browser connection: refs, viewport ids and coordinates from then are invalid, and the page may have been changed outside Atlas. Observe the page and verify the requirements marked reverify again before reporting them done."
	}
	data, _ := json.Marshal(out)
	return string(data)
}

// BrowserTaskContinuation reports whether a finished turn left the active
// browser task with open requirements that work can still advance. Stale
// evidence and blockers from an earlier process count as open. It returns
// the continuation prompt and records the continuation; it stops (and
// records why) after two continuations in a row without progress, or at the
// overall bound. Progress is a new distinct successful page change or a
// newly closed requirement, never a repeated read or a failure.
func BrowserTaskContinuation(root, sessionID string) (string, bool) {
	d := BrowserTaskContinue(root, sessionID)
	return d.Prompt, d.Continue
}

// BrowserContinuation is the end-of-turn decision for the active browser
// task: continue with Prompt, or stop for StopReason (empty when there is
// no open work at all).
type BrowserContinuation struct {
	Prompt          string
	Continue        bool
	StopReason      string
	TaskID          string
	Open            int
	DeliveryPending int
}

// Stop reasons recorded when a turn ends for something other than the
// contract's own continuation bounds. They keep "the user stopped it",
// "it waits for the user", "the provider failed" and "no progress" apart.
const (
	BrowserStopUserCanceled   = "user_canceled"
	BrowserStopWaitingForUser = "waiting_for_user"
	BrowserStopProviderError  = "provider_error"
	BrowserStopStepLimit      = "step_limit"
	BrowserStopRepeatedCalls  = "repeated_tool_calls"
	BrowserStopUserDirection  = "user_direction"
)

// RecordBrowserTaskStop records why a turn ended while the active task
// still had open requirements, so the next turn and the user see the real
// reason instead of a guess. It never closes a requirement.
func RecordBrowserTaskStop(root, sessionID, reason, detail string) {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return
	}
	if open, _, _ := file.Active.counts(); open == 0 {
		return
	}
	file.Active.StopReason = reason
	if detail = strings.TrimSpace(detail); detail != "" {
		file.Active.StopReason += ": " + truncateRunes(detail, 200)
	}
	_ = saveBrowserTasks(root, sessionID, file)
}

// BrowserTaskPromptBlock renders the active contract for the system prompt.
// It is read from the checkpoint on every request, so summarizing the
// conversation or switching the model cannot drop the goal, the
// requirements, their verification state or the remaining work.
func BrowserTaskPromptBlock(root, sessionID string) string {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return ""
	}
	task := file.Active
	open, verified, blocked := task.counts()
	if open == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n<browser_task id=%q open=%d verified=%d blocked=%d>\n", task.ID, open, verified, blocked)
	fmt.Fprintf(&b, "Goal: %s\n", task.Goal)
	for _, r := range task.Requirements {
		status, reason := task.effective(r)
		line := fmt.Sprintf("- %s [%s", r.ID, status)
		if reason != "" {
			line += ", verify again: " + reason
		}
		if r.Blocker != "" {
			line += ", blocker: " + r.Blocker
		}
		if r.Expect != "" {
			line += fmt.Sprintf(", evidence must show %q", r.Expect)
		}
		if r.Subject != "" {
			line += fmt.Sprintf(", subject %q from %s", r.Subject, r.Source)
		}
		fmt.Fprintf(&b, "%s, needs %s evidence] %s\n", line, r.Verify, truncateRunes(r.Text, 160))
	}
	if task.StopReason != "" {
		fmt.Fprintf(&b, "Last stop: %s\n", task.StopReason)
	}
	b.WriteString("This contract is the user's authorized task and survives summaries and model changes. When the user says to continue, or a turn starts with " + BrowserContinuationMarker + ", resume the open requirements without asking again: call task_status and work on the next open requirement. Structural and visual requirements need fresh page evidence; an existing frame or layer is not evidence that the requested design is finished. Observation requirements need returned subject and claim text from their declared source and retain that historical evidence across navigation. Delivery requirements are fulfilled only by the saved final response; once browser work is ready, compose the requested output. Report open requirements as open, never as delivered.\n</browser_task>")
	return b.String()
}

// BrowserTaskContinue decides the end of a turn for the active task.
func BrowserTaskContinue(root, sessionID string) BrowserContinuation {
	browserTaskMu.Lock()
	defer browserTaskMu.Unlock()
	file, err := loadBrowserTasks(root, sessionID)
	if err != nil || file == nil || file.Active == nil {
		return BrowserContinuation{}
	}
	task := file.Active
	totalOpen, _, _ := task.counts()
	_, deliveryPending := task.actionableCounts()
	var open []string
	closed := 0
	for _, r := range task.Requirements {
		status, reason := task.effective(r)
		switch status {
		case ReqVerified, ReqBlocked:
			closed++
		default:
			if r.Verify == "delivery" {
				continue
			}
			line := fmt.Sprintf("%s (%s): %s", r.ID, status, r.Text)
			if r.Verify == "observation" {
				line += fmt.Sprintf(" [source %s, subject %q, expected claim %q]", r.Source, r.Subject, r.Expect)
			}
			if reason != "" {
				line += " [verify again: " + reason + "]"
			}
			open = append(open, line)
		}
	}
	if len(open) == 0 {
		return BrowserContinuation{TaskID: task.ID, Open: totalOpen, DeliveryPending: deliveryPending}
	}
	stop := func(reason string) BrowserContinuation {
		task.StopReason = reason
		_ = saveBrowserTasks(root, sessionID, file)
		slog.Info("Browser task auto-continuation stopped", "task", task.ID, "reason", reason, "open", len(open))
		return BrowserContinuation{StopReason: reason, TaskID: task.ID, Open: totalOpen, DeliveryPending: deliveryPending}
	}
	if task.Continuations >= maxAutoContinuations {
		return stop(fmt.Sprintf("continuation_limit: %d automatic continuations reached with %d requirement(s) open", maxAutoContinuations, len(open)))
	}
	mark := fmt.Sprintf("%d/%d", task.Progress, closed)
	if mark == task.ProgressSignature {
		task.NoProgressContinuation++
	} else {
		task.NoProgressContinuation = 0
	}
	task.ProgressSignature = mark
	if task.NoProgressContinuation >= maxNoProgressContinuation {
		return stop(fmt.Sprintf("no_progress: %d continuations in a row without a new successful page change or newly closed requirement", task.NoProgressContinuation))
	}
	task.Continuations++
	task.StopReason = ""
	if err := saveBrowserTasks(root, sessionID, file); err != nil {
		return BrowserContinuation{StopReason: "checkpoint_unwritable: " + err.Error(), TaskID: task.ID, Open: totalOpen, DeliveryPending: deliveryPending}
	}
	prompt := fmt.Sprintf("%s The browser task %q still has %d actionable open requirement(s):\n- %s\nContinue the work now without asking for confirmation. Structural and visual requirements need current page observations after the last change to their target. Observation requirements need returned subject and claim text from their declared source; navigation does not invalidate those historical observations. Mark requirements verified with the corresponding evidence op_ids. Pending delivery requirements await the saved final response after the browser work. If one cannot proceed for a concrete reason, record it with task_update status=blocked, a blocker code and its evidence or note; length or number of steps is not a blocker. If the user has since asked for different work, start that with task_plan mode=new instead.", BrowserContinuationMarker, task.ID, len(open), strings.Join(open, "\n- "))
	return BrowserContinuation{Prompt: prompt, Continue: true, TaskID: task.ID, Open: totalOpen, DeliveryPending: deliveryPending}
}

func (t *browserTask) actionableCounts() (work, delivery int) {
	for _, r := range t.Requirements {
		status, _ := t.effective(r)
		if status == ReqVerified || status == ReqBlocked {
			continue
		}
		if r.Verify == "delivery" {
			delivery++
		} else {
			work++
		}
	}
	return work, delivery
}
