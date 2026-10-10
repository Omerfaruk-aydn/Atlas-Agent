// Package outcome is the small shared vocabulary that separates a tool call
// returning from the requested work being done. It has no knowledge of any
// particular tool: adapters report what they observed, and the ledger keeps
// each claim bound to its source, its subject and the changes that void it.
package outcome

import (
	"slices"
	"strings"
	"time"
)

// Phase is how far one piece of work got. The values are deliberately not
// ordered into a single success scale: unknown, blocked and canceled are
// not partial success.
type Phase string

const (
	PhasePlanned    Phase = "planned"
	PhaseDispatched Phase = "dispatched"
	// PhaseUnknown means input may have been sent but the result was never
	// seen. A mutation in this phase must be observed, never replayed.
	PhaseUnknown   Phase = "unknown"
	PhaseApplied   Phase = "applied"
	PhaseVerified  Phase = "verified"
	PhaseDelivered Phase = "delivered"
	PhaseBlocked   Phase = "blocked"
	PhaseCanceled  Phase = "canceled"
	PhaseFailed    Phase = "failed"
)

// Terminal reports whether no further execution is expected for the phase
// without outside input.
func (p Phase) Terminal() bool {
	switch p {
	case PhaseVerified, PhaseDelivered, PhaseBlocked, PhaseCanceled, PhaseFailed:
		return true
	}
	return false
}

// Result is what an adapter observed about one dispatch.
type Result struct {
	Canceled bool
	// Err is a transport or timeout failure: the call did not report.
	Err error
	// Rejected is a tool-reported error. Rejected without Sent means the
	// tool refused before sending anything.
	Rejected bool
	Sent     bool
	// Uncertain is explicit adapter evidence that a sent mutation's effect
	// remains unknown, including while a question or permission is pending.
	Uncertain bool
	// DispatchKnown distinguishes an adapter's no-input refusal from an
	// opaque transport error whose dispatch cannot be established.
	DispatchKnown bool
	// Waiting is a stop for the user: a question, permission or handoff.
	Waiting  bool
	Mutation bool
	// Observed is a tool-owned check of the outcome (exit code, read-back,
	// contract) that passed. Without it success is only applied.
	Observed bool
}

// Classify maps an observation to a phase. The tool answering "ok" yields
// applied; only an observed check yields verified.
func Classify(r Result) Phase {
	switch {
	case r.Uncertain && r.Mutation:
		return PhaseUnknown
	case r.Canceled:
		if r.Mutation && (!r.DispatchKnown || r.Sent) {
			return PhaseUnknown
		}
		return PhaseCanceled
	case r.Waiting:
		return PhaseBlocked
	case r.Err != nil:
		if r.Mutation && (!r.DispatchKnown || r.Sent) {
			return PhaseUnknown
		}
		return PhaseFailed
	case r.Rejected:
		if r.Mutation && r.Sent {
			return PhaseUnknown
		}
		return PhaseFailed
	case r.Observed:
		return PhaseVerified
	case r.Mutation:
		return PhaseApplied
	}
	return PhaseApplied
}

// Kind separates what a piece of evidence can be used for.
type Kind string

const (
	// KindAuthority permits sending input to a live target.
	KindAuthority Kind = "authority"
	// KindObservation is a dated record of something that was read.
	KindObservation Kind = "observation"
	// KindOutput verifies a produced file, state or result.
	KindOutput Kind = "output"
	// KindDelivery is a final answer or file actually handed over.
	KindDelivery Kind = "delivery"
)

// Evidence is one claim with its provenance.
type Evidence struct {
	ID         string `json:"id"`
	Kind       Kind   `json:"kind"`
	Source     string `json:"source"`
	Subject    string `json:"subject"`
	Claim      string `json:"claim"`
	ObservedAt int64  `json:"observed_at"`
	// StaleBy names the source whose later change voided an output or
	// authority record. Observations are history and are never voided.
	StaleBy string `json:"stale_by,omitempty"`
}

// Record is one dispatch and what became of it.
type Record struct {
	OpID     string `json:"op_id"`
	Tool     string `json:"tool"`
	Phase    Phase  `json:"phase"`
	Mutation bool   `json:"mutation,omitempty"`
	// Sent preserves known partial effects while a later step waits for the
	// user. A blocked call with no dispatched input leaves evidence intact.
	Sent     bool     `json:"sent,omitempty"`
	Subjects []string `json:"subjects,omitempty"`
	// Fingerprint identifies the exact call so an identical resend of an
	// unknown-outcome mutation can be recognised.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Observed is retained for old checkpoints; it never authorizes replay.
	Observed bool `json:"observed,omitempty"`
	// Resolved records explicit reconciliation and prevents late responses
	// from reviving an already inspected uncertain operation.
	Resolved bool  `json:"resolved,omitempty"`
	At       int64 `json:"at"`
}

const (
	maxRecords  = 128
	maxEvidence = 256
)

// Ledger holds the records and evidence of one session. The zero value is
// ready to use.
type Ledger struct {
	LastMutationAt int64        `json:"last_mutation_at,omitempty"`
	Records        []Record     `json:"records,omitempty"`
	Evidence       []Evidence   `json:"evidence,omitempty"`
	Continuation   Continuation `json:"continuation,omitempty"`
}

// Continuation retains automatic continuation bounds across restarts.
type Continuation struct {
	ContractFingerprint string `json:"contract_fingerprint,omitempty"`
	ProgressSignature   string `json:"progress_signature,omitempty"`
	Count               int    `json:"count,omitempty"`
	NoProgress          int    `json:"no_progress,omitempty"`
	StopReason          string `json:"stop_reason,omitempty"`
}

// Record stores a dispatch outcome. A mutation that was applied or may have
// been applied voids earlier output and authority evidence about the same
// subjects, so a later check has to be repeated against the new state.
func (l *Ledger) Record(r Record) {
	for i := range l.Records {
		if r.OpID != "" && l.Records[i].OpID == r.OpID {
			if l.Records[i].Resolved {
				return
			}
			if r.At == 0 {
				r.At = l.Records[i].At
			}
			l.Records = slices.Delete(l.Records, i, i+1)
			break
		}
	}
	if r.At == 0 {
		r.At = time.Now().UnixMilli()
	}
	if r.Mutation && (r.Phase == PhaseDispatched || r.Phase == PhaseApplied || r.Phase == PhaseVerified || r.Phase == PhaseUnknown || r.Phase == PhaseBlocked && r.Sent) {
		l.LastMutationAt = max(l.LastMutationAt, r.At)
		l.invalidate(r.OpID, r.Subjects)
	}
	l.Records = append(l.Records, r)
	if len(l.Records) > maxRecords {
		kept := l.Records[:0]
		drop := len(l.Records) - maxRecords
		for _, rec := range l.Records {
			// Unresolved outcomes and reconciliation tombstones are retained:
			// a late response must not revive an inspected operation.
			if drop > 0 && !rec.Resolved && rec.Phase != PhaseUnknown && rec.Phase != PhaseDispatched {
				drop--
				continue
			}
			kept = append(kept, rec)
		}
		l.Records = kept
	}
}

// Add stores evidence. Evidence needs a source and a claim to support
// anything, and a subject to be voidable.
func (l *Ledger) Add(e Evidence) bool {
	if e.Source == "" || e.Claim == "" {
		return false
	}
	if e.ObservedAt == 0 {
		e.ObservedAt = time.Now().UnixMilli()
	}
	l.Evidence = append(l.Evidence, e)
	if len(l.Evidence) > maxEvidence {
		l.Evidence = slices.Clone(l.Evidence[len(l.Evidence)-maxEvidence:])
	}
	return true
}

func (l *Ledger) invalidate(source string, subjects []string) {
	for i := range l.Evidence {
		e := &l.Evidence[i]
		if e.StaleBy != "" || e.Kind == KindObservation || e.Kind == KindDelivery {
			continue
		}
		if e.Subject != "" && (slices.Contains(subjects, "*") || slices.Contains(subjects, e.Subject)) {
			e.StaleBy = source
		}
	}
}

// Supports reports whether current, unvoided evidence of the given kind
// backs the claim for the subject.
func (l *Ledger) Supports(kind Kind, subject, claim string) bool {
	return slices.ContainsFunc(l.Evidence, func(e Evidence) bool {
		return e.Kind == kind && e.Subject == subject && e.Claim == claim && e.StaleBy == ""
	})
}

// Unresolved lists mutations whose result was never seen.
func (l *Ledger) Unresolved() []Record {
	var out []Record
	for _, r := range l.Records {
		if r.Phase == PhaseUnknown {
			out = append(out, r)
		}
	}
	return out
}

// Resolve reconciles an unknown or running outcome after inspection,
// recording what the observation found.
func (l *Ledger) Resolve(opID string, phase Phase) bool {
	if phase != PhaseApplied && phase != PhaseVerified && phase != PhaseFailed && phase != PhaseCanceled {
		return false
	}
	resolved := false
	for i := range l.Records {
		if l.Records[i].OpID == opID && (l.Records[i].Phase == PhaseUnknown || l.Records[i].Phase == PhaseDispatched) {
			l.Records[i].Phase = phase
			l.Records[i].Resolved = true
			resolved = true
		}
	}
	return resolved
}

// Requirement is one stated piece of the user's request.
type Requirement struct {
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Phase       Phase    `json:"phase"`
	// Reason is the concrete technical cause when the phase is blocked or
	// failed.
	Reason string `json:"reason,omitempty"`
}

// State is a requirement's standing once its dependencies are considered.
type State string

const (
	StateReady       State = "ready"
	StateDone        State = "done"
	StateWaiting     State = "waiting_on_dependency"
	StateBlockedDep  State = "blocked_by_dependency"
	StateUnresolved  State = "outcome_unknown"
	StateBlocked     State = "blocked"
	StateFailed      State = "failed"
	StateCanceled    State = "canceled"
	StateUnknownDeps State = "unknown_dependency"
)

// Resolution is one requirement's effective state.
type Resolution struct {
	ID     string `json:"id"`
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
}

func done(p Phase) bool { return p == PhaseVerified || p == PhaseDelivered }

// Resolve computes each requirement's state. A requirement whose upstream is
// blocked, failed or canceled is not executable and must not generate tool
// calls; one whose upstream is still open waits; an upstream that is merely
// applied (not verified) also keeps dependents waiting, since a dependent
// check cannot rest on unverified output.
func Resolve(reqs []Requirement) []Resolution {
	byID := make(map[string]Requirement, len(reqs))
	for _, r := range reqs {
		byID[r.ID] = r
	}
	memo := map[string]Resolution{}
	var walk func(id string, seen []string) Resolution
	walk = func(id string, seen []string) Resolution {
		if got, ok := memo[id]; ok {
			return got
		}
		r := byID[id]
		res := Resolution{ID: id}
		switch {
		case slices.Contains(seen, id):
			res.State, res.Detail = StateFailed, "dependency cycle"
		case r.Phase == PhaseUnknown:
			res.State, res.Detail = StateUnresolved, "observe the result before acting again"
		case r.Phase == PhaseCanceled:
			res.State = StateCanceled
		case r.Phase == PhaseBlocked:
			res.State, res.Detail = StateBlocked, r.Reason
		case r.Phase == PhaseFailed:
			res.State, res.Detail = StateFailed, r.Reason
		default:
			res.State = StateReady
			if done(r.Phase) {
				res.State = StateDone
			}
			for _, dep := range r.DependsOn {
				if _, ok := byID[dep]; !ok {
					res.State, res.Detail = StateUnknownDeps, "unknown dependency "+dep
					break
				}
				up := walk(dep, append(seen, id))
				switch up.State {
				case StateDone:
				case StateBlocked, StateFailed, StateCanceled, StateBlockedDep, StateUnknownDeps:
					res.State, res.Detail = StateBlockedDep, dep+" is "+string(up.State)
				default:
					if res.State == StateReady || res.State == StateDone {
						res.State, res.Detail = StateWaiting, dep+" is not verified"
					}
				}
				if res.State == StateBlockedDep {
					break
				}
			}
		}
		memo[id] = res
		return res
	}
	out := make([]Resolution, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, walk(r.ID, nil))
	}
	return out
}

// Executable lists requirements that have work that can run now.
func Executable(reqs []Requirement) []string {
	var ids []string
	for _, res := range Resolve(reqs) {
		if res.State == StateReady {
			ids = append(ids, res.ID)
		}
	}
	return ids
}

// Report is the honest account a final message may be built from.
type Report struct {
	Verified   []string `json:"verified,omitempty"`
	Unverified []string `json:"unverified,omitempty"`
	Blocked    []string `json:"blocked,omitempty"`
	Unknown    []string `json:"unknown,omitempty"`
	Failed     []string `json:"failed,omitempty"`
	Canceled   []string `json:"canceled,omitempty"`
	Open       []string `json:"open,omitempty"`
}

// Complete reports whether everything the user asked for is verified. Blocked
// or failed work is a legitimate end state, but it is never "complete".
func (r Report) Complete() bool {
	return len(r.Unverified)+len(r.Blocked)+len(r.Unknown)+len(r.Failed)+len(r.Canceled)+len(r.Open) == 0
}

// Summarize groups requirements by what can honestly be said about them.
func Summarize(reqs []Requirement) Report {
	var rep Report
	for i, res := range Resolve(reqs) {
		label := res.ID
		if d := strings.TrimSpace(reqs[i].Description); d != "" {
			label += ": " + d
		}
		switch res.State {
		case StateDone:
			rep.Verified = append(rep.Verified, label)
		case StateUnresolved:
			rep.Unknown = append(rep.Unknown, label)
		case StateCanceled:
			rep.Canceled = append(rep.Canceled, label)
		case StateBlocked, StateBlockedDep, StateUnknownDeps:
			if res.Detail != "" {
				label += " (" + res.Detail + ")"
			}
			rep.Blocked = append(rep.Blocked, label)
		case StateFailed:
			if res.Detail != "" {
				label += " (" + res.Detail + ")"
			}
			rep.Failed = append(rep.Failed, label)
		default:
			if reqs[i].Phase == PhaseApplied {
				rep.Unverified = append(rep.Unverified, label)
			} else {
				rep.Open = append(rep.Open, label)
			}
		}
	}
	return rep
}

// Block renders the report for the model so the final message states what
// was and was not achieved. It returns "" when nothing needs saying.
func (r Report) Block() string {
	if r.Complete() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<outcome_state>\nRecorded outcomes, not instructions. The final message must state what is verified and what is not, and must not claim more.\n")
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		sb.WriteString(title + ":\n")
		for _, item := range items {
			sb.WriteString("- " + item + "\n")
		}
	}
	list("Verified", r.Verified)
	list("Applied but not verified", r.Unverified)
	list("Outcome unknown (observe before acting again; never resend)", r.Unknown)
	list("Blocked", r.Blocked)
	list("Failed", r.Failed)
	list("Canceled", r.Canceled)
	list("Not started", r.Open)
	if !r.Complete() {
		sb.WriteString("Do not describe this work as complete, working or tested beyond the verified items.\n")
	}
	sb.WriteString("</outcome_state>")
	return sb.String()
}

// UnresolvedCall returns the unobserved unknown-outcome record of the exact
// same call, if one exists.
func (l *Ledger) UnresolvedCall(fingerprint string) (Record, bool) {
	if fingerprint == "" {
		return Record{}, false
	}
	for _, r := range l.Unresolved() {
		if r.Fingerprint == fingerprint {
			return r, true
		}
	}
	return Record{}, false
}

// MarkObserved retains the historical observation flag. Looking at something
// is not reconciliation: the unknown result remains blocked until Resolve.
func (l *Ledger) MarkObserved(opID string) {
	for i := range l.Records {
		if l.Records[i].OpID == opID {
			l.Records[i].Observed = true
		}
	}
}

// Requirements turns unresolved mutations into requirements, so the report
// names them next to any other recorded work.
func (l *Ledger) Requirements() []Requirement {
	var out []Requirement
	for _, r := range l.Unresolved() {
		out = append(out, Requirement{ID: r.Tool + "#" + r.OpID, Description: strings.Join(r.Subjects, ", "), Phase: PhaseUnknown})
	}
	for _, r := range l.Records {
		if r.Phase == PhaseDispatched && r.Mutation {
			out = append(out, Requirement{ID: r.Tool + "#" + r.OpID, Description: "Background operation is running; observe its job output before depending on its result", Phase: PhaseDispatched})
		}
	}
	return out
}
