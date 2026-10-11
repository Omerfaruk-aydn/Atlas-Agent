package support

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

const permissionsPath = "/v1/support/permissions"

// Permission request states.
const (
	PermissionPending  = "pending"
	PermissionGranted  = "granted"
	PermissionDenied   = "denied"
	PermissionCanceled = "canceled"
)

// PermissionAsk is what the supporter's agent sends to ask the computer
// owner for more capabilities.
type PermissionAsk struct {
	Capabilities []Capability `json:"capabilities"`
	Reason       string       `json:"reason"`
}

func writeSupportJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleRequestPermission records a request for the owner. It never
// changes capabilities by itself; only AnswerPermission on this computer
// can, and only for the capabilities that were asked for.
func (h *Host) handleRequestPermission(w http.ResponseWriter, r *http.Request) {
	g, apiErr, status := h.Authorize(r, nodes.NeedInfo)
	if apiErr != nil {
		writeSupportJSON(w, status, apiErr)
		return
	}
	var ask PermissionAsk
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&ask); err != nil {
		writeSupportJSON(w, http.StatusBadRequest, apiErr2(CodeProtocol, "invalid permission request"))
		return
	}
	caps := Normalize(ask.Capabilities)
	if len(caps) == 0 || len(caps) != len(ask.Capabilities) {
		writeSupportJSON(w, http.StatusBadRequest, apiErr2(CodeProtocol, "name one or more known capabilities"))
		return
	}
	reason := sanitizeName(ask.Reason, "")
	if reason == "" {
		writeSupportJSON(w, http.StatusBadRequest, apiErr2(CodeProtocol, "a permission request needs a reason the owner can read"))
		return
	}
	h.mu.Lock()
	s := h.session
	if s == nil || s.id != g.SessionID {
		h.mu.Unlock()
		writeSupportJSON(w, http.StatusUnauthorized, apiErr2(CodeSessionRevoked, "the session ended"))
		return
	}
	var missing []Capability
	for _, c := range caps {
		if !slices.Contains(s.caps, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		h.mu.Unlock()
		writeSupportJSON(w, http.StatusOK, PermissionRequest{State: PermissionGranted, Capabilities: caps, SessionID: s.id})
		return
	}
	revision := 1
	if prev := h.permission; prev != nil {
		// A new request supersedes the previous one; an answer meant for
		// the old one no longer matches.
		revision = prev.Revision + 1
		if prev.State == PermissionPending {
			prev.State = PermissionCanceled
		}
	}
	req := &PermissionRequest{ID: "perm-" + randomID(6), Revision: revision, Kind: "capabilities", Capabilities: missing, Reason: reason, State: PermissionPending, CreatedAt: time.Now(), SessionID: s.id}
	h.permission = req
	out := *req
	h.mu.Unlock()
	_ = h.history.Append(Event{SessionID: g.SessionID, Role: "host", Kind: "permission_requested", Detail: reason, Fields: map[string]string{"capabilities": strings.Join(Strings(missing), ","), "request": req.ID}})
	h.notify()
	writeSupportJSON(w, http.StatusAccepted, out)
}

func (h *Host) handlePermissionStatus(w http.ResponseWriter, r *http.Request) {
	g, apiErr, status := h.Authorize(r, nodes.NeedInfo)
	if apiErr != nil {
		writeSupportJSON(w, status, apiErr)
		return
	}
	h.mu.Lock()
	p := h.permission
	var out PermissionRequest
	if p != nil && p.ID == r.PathValue("id") && p.SessionID == g.SessionID {
		out = *p
	}
	h.mu.Unlock()
	if out.ID == "" {
		writeSupportJSON(w, http.StatusNotFound, apiErr2(CodeProtocol, "no such permission request in this session"))
		return
	}
	writeSupportJSON(w, http.StatusOK, out)
}

func (h *Host) handleCancelPermission(w http.ResponseWriter, r *http.Request) {
	g, apiErr, status := h.Authorize(r, nodes.NeedInfo)
	if apiErr != nil {
		writeSupportJSON(w, status, apiErr)
		return
	}
	h.mu.Lock()
	p := h.permission
	var out PermissionRequest
	if p != nil && p.ID == r.PathValue("id") && p.SessionID == g.SessionID {
		if p.State == PermissionPending {
			p.State = PermissionCanceled
		}
		out = *p
	}
	h.mu.Unlock()
	if out.ID == "" {
		writeSupportJSON(w, http.StatusNotFound, apiErr2(CodeProtocol, "no such permission request in this session"))
		return
	}
	h.notify()
	writeSupportJSON(w, http.StatusOK, out)
}

// ErrStaleAnswer is returned for an answer to a request that was
// canceled, superseded or already answered; nothing changes.
var ErrStaleAnswer = errors.New("that permission request is no longer pending")

// AnswerPermission applies the owner's answer to exactly the request id
// and revision shown to them.
func (h *Host) AnswerPermission(id string, revision int, grant bool) error {
	h.mu.Lock()
	p := h.permission
	s := h.session
	if p == nil || s == nil || p.ID != id || p.Revision != revision || p.State != PermissionPending || p.SessionID != s.id {
		h.mu.Unlock()
		return ErrStaleAnswer
	}
	kind := "permission_denied"
	if grant {
		p.State = PermissionGranted
		s.caps = Normalize(append(slices.Clone(s.caps), p.Capabilities...))
		kind = "permission_granted"
	} else {
		p.State = PermissionDenied
	}
	sessionID, caps := s.id, strings.Join(Strings(p.Capabilities), ",")
	h.mu.Unlock()
	_ = h.history.Append(Event{SessionID: sessionID, Role: "host", Kind: kind, Fields: map[string]string{"capabilities": caps, "request": id}})
	h.notify()
	return nil
}

func apiErr2(code Code, msg string) *nodes.APIError {
	return &nodes.APIError{Code: string(code), Message: msg, Effect: "none"}
}
