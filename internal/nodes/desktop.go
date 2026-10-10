package nodes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

var pointerActions = []string{"click", "double_click", "right_click", "move", "drag", "scroll"}

var inputActions = append([]string{"type", "key", "hotkey"}, pointerActions...)

// readOnlyAutomation are the accessibility queries a controller may run on a
// node desktop without an operation id; none of them send input.
var readOnlyAutomation = []string{"windows", "inspect", "find", "monitors"}

// isBlank reports whether a capture is a single solid color, which is what
// a disconnected or minimized remote session produces.
func isBlank(data []byte) bool {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	b := img.Bounds()
	if b.Empty() {
		return true
	}
	first := img.At(b.Min.X, b.Min.Y)
	r0, g0, b0, _ := first.RGBA()
	stepX, stepY := max(1, b.Dx()/64), max(1, b.Dy()/64)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r, g, bb, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bb != b0 {
				return false
			}
		}
	}
	return true
}

func (s *Server) handleObserve(w http.ResponseWriter, r *http.Request) {
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	size, err := s.opts.Desktop.ScreenSize()
	if err != nil {
		writeError(w, http.StatusConflict, "capture_failed", err.Error(), false, "none")
		return
	}
	data, err := s.opts.Desktop.Screenshot()
	if err != nil {
		writeError(w, http.StatusConflict, "capture_failed", err.Error(), false, "none")
		return
	}
	obs := DesktopObservation{ObservationID: "obs-" + randomHex(8), Width: size.Width, Height: size.Height, CapturedAt: time.Now().UnixMilli(), Blank: isBlank(data)}
	if r.URL.Query().Get("image") != "0" {
		obs.PNG = data
	}
	if p, err := s.opts.Desktop.CursorPosition(); err == nil {
		obs.Cursor = [2]int{p.X, p.Y}
	}
	if auto, ok := s.opts.Desktop.(computer.AutomationBackend); ok {
		if raw, err := auto.Automation(r.Context(), computer.AutomationRequest{Action: "windows"}); err == nil {
			obs.Windows = raw
		}
	}
	s.obs = &observationState{id: obs.ObservationID, size: size, at: time.Now()}
	writeJSON(w, http.StatusOK, obs)
}

// checkObservation enforces that input is aimed using the latest
// observation of the current screen geometry.
func (s *Server) checkObservation(id string, pointer bool) error {
	if !pointer && id == "" {
		return nil
	}
	if s.obs == nil || s.obs.id != id {
		return errors.New("observation_id is not the latest observation of this desktop; observe again")
	}
	if time.Since(s.obs.at) > observationTTL {
		return errors.New("observation is older than two minutes; observe again")
	}
	size, err := s.opts.Desktop.ScreenSize()
	if err != nil {
		return err
	}
	if size != s.obs.size {
		return fmt.Errorf("screen changed from %dx%d to %dx%d since the observation", s.obs.size.Width, s.obs.size.Height, size.Width, size.Height)
	}
	return nil
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var in DesktopInput
	if !decode(w, r, &in) {
		return
	}
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	if !slices.Contains(inputActions, in.Action) {
		writeError(w, http.StatusBadRequest, "invalid_action", "action must be one of "+strings.Join(inputActions, ", "), false, "none")
		return
	}
	pointer := slices.Contains(pointerActions, in.Action)
	d := deviceFrom(r)
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	if replay, err := s.journal.Replay(in.OperationID, d.ID, RequestHash(in)); err != nil {
		writeError(w, http.StatusConflict, "operation_conflict", err.Error(), false, "none")
		return
	} else if replay != nil {
		writeJSON(w, http.StatusOK, replay)
		return
	}
	if err := s.checkObservation(in.ObservationID, pointer); err != nil {
		writeError(w, http.StatusConflict, "stale_observation", err.Error(), false, "none")
		return
	}
	if in.WindowID != "" {
		foreground, ok := s.opts.Desktop.(interface{ ForegroundWindow() string })
		if !ok || foreground.ForegroundWindow() != in.WindowID {
			writeError(w, http.StatusConflict, "wrong_window", "the expected window is not the verified foreground window; observe again before input", false, "none")
			return
		}
	}
	if pointer {
		size := s.obs.size
		points := [][2]int{{in.X, in.Y}}
		if in.Action == "drag" {
			points = append(points, [2]int{in.EndX, in.EndY})
		}
		for _, p := range points {
			if err := computer.ValidatePointIn(size, p[0], p[1]); err != nil {
				writeError(w, http.StatusBadRequest, "out_of_bounds", err.Error(), false, "none")
				return
			}
		}
	}
	op, existing, err := s.journal.Begin(Operation{ID: in.OperationID, Kind: "desktop_" + in.Action, DeviceID: d.ID, RequestHash: RequestHash(in)})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrOperationConflict) {
			status = http.StatusConflict
		}
		writeError(w, status, "operation_conflict", err.Error(), false, "none")
		return
	}
	if existing {
		writeJSON(w, http.StatusOK, op)
		return
	}
	ierr := s.sendInput(in)
	final, ferr := s.journal.Finish(op.ID, func(o *Operation) {
		if ierr != nil {
			o.State, o.Effect, o.Error = taskstate.Failed, "unknown", ierr.Error()
			return
		}
		o.State, o.Effect = taskstate.Completed, "applied"
	})
	if pointer {
		// Pointer input may change the layout; the next pointer action needs
		// a fresh observation.
		s.obs = nil
	}
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), true, "unknown")
		return
	}
	writeJSON(w, http.StatusOK, final)
}

func (s *Server) sendInput(in DesktopInput) error {
	b := s.opts.Desktop
	switch in.Action {
	case "click", "right_click":
		button := in.Button
		if in.Action == "right_click" {
			button = "right"
		}
		mb, err := computer.ParseButton(button)
		if err != nil {
			return err
		}
		return b.Click(in.X, in.Y, mb)
	case "double_click":
		return b.DoubleClick(in.X, in.Y)
	case "move":
		return b.MoveTo(in.X, in.Y)
	case "drag":
		return b.Drag(in.X, in.Y, in.EndX, in.EndY)
	case "scroll":
		if err := b.MoveTo(in.X, in.Y); err != nil {
			return err
		}
		return b.Scroll(in.ScrollX, in.ScrollY)
	case "type":
		if in.Text == "" || len(in.Text) > 8192 {
			return errors.New("type needs 1-8192 bytes of text")
		}
		return b.TypeText(in.Text)
	case "key":
		if _, ok := computer.ResolveKey(in.Key); !ok {
			return fmt.Errorf("unknown key %q", in.Key)
		}
		return b.KeyPress(in.Key)
	case "hotkey":
		mods, err := computer.ParseModifiers(strings.Split(in.Modifiers, "+"))
		if err != nil {
			return err
		}
		return b.Hotkey(mods, in.Key)
	}
	return fmt.Errorf("unsupported action %s", in.Action)
}

func (s *Server) handleAutomation(w http.ResponseWriter, r *http.Request) {
	var req AutomationRequest
	if !decode(w, r, &req) {
		return
	}
	if !slices.Contains(readOnlyAutomation, req.Action) {
		writeError(w, http.StatusBadRequest, "invalid_action", "automation action must be one of "+strings.Join(readOnlyAutomation, ", "), false, "none")
		return
	}
	auto, ok := s.opts.Desktop.(computer.AutomationBackend)
	if !ok {
		writeError(w, http.StatusConflict, "automation_unavailable", "accessibility automation is not available on this node", false, "none")
		return
	}
	areq := computer.AutomationRequest{Action: req.Action, WindowID: req.WindowID, Name: req.Name, Role: req.Role, MaxElements: req.MaxElements}
	if err := computer.ValidateAutomationRequest(areq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), false, "none")
		return
	}
	s.desktopMu.Lock()
	raw, err := auto.Automation(r.Context(), areq)
	s.desktopMu.Unlock()
	if err != nil {
		writeError(w, http.StatusConflict, "automation_failed", err.Error(), false, "none")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(json.RawMessage(raw))
}

// LaunchRequest starts a GUI program in the node's desktop session.
type LaunchRequest struct {
	OperationID string   `json:"operation_id"`
	Argv        []string `json:"argv"`
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req LaunchRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Argv) == 0 || req.Argv[0] == "" || len(req.Argv) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_request", "argv must contain 1-64 literal arguments", false, "none")
		return
	}
	d := deviceFrom(r)
	op, existing, err := s.journal.Begin(Operation{ID: req.OperationID, Kind: "desktop_launch", DeviceID: d.ID, RequestHash: RequestHash(req), Argv: req.Argv})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrOperationConflict) {
			status = http.StatusConflict
		}
		writeError(w, status, "operation_conflict", err.Error(), false, "none")
		return
	}
	if existing {
		writeJSON(w, http.StatusOK, op)
		return
	}
	dir, _ := s.resolveDir("")
	proc, lerr := procown.Start(procown.Spec{Argv: req.Argv, Dir: dir, Visible: true})
	final, ferr := s.journal.Finish(op.ID, func(o *Operation) {
		if lerr != nil {
			o.State, o.Effect, o.Error = taskstate.Failed, "none", lerr.Error()
			return
		}
		o.State, o.Effect = taskstate.Completed, "applied"
		o.Stdout = fmt.Sprintf("pid=%d created_at=%d", proc.Identity.PID, proc.Identity.CreatedAt)
	})
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), true, "unknown")
		return
	}
	writeJSON(w, http.StatusOK, final)
}
