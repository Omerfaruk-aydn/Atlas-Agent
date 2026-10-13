package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
var readOnlyAutomation = []string{"windows", "inspect", "find", "monitors", "apps"}

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
	g := grantFrom(r)
	obs := DesktopObservation{ObservationID: "obs-" + randomHex(8), NodeID: s.id.NodeID, SessionID: g.SessionID, Generation: g.Generation, Monitor: "virtual", CoordinateSpace: "virtual_screen_physical_pixels", Scale: 1, Width: size.Width, Height: size.Height, CapturedAt: time.Now().UnixMilli(), Blank: isBlank(data)}
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
		if raw, err := auto.Automation(r.Context(), computer.AutomationRequest{Action: "monitors"}); err == nil {
			obs.Monitors = raw
		}
	}
	s.consumed = nil
	s.obs = &observationState{id: obs.ObservationID, size: size, at: time.Now(), session: g.SessionID, generation: g.Generation}
	writeJSON(w, http.StatusOK, obs)
}

// checkObservation enforces that input is aimed using the latest
// observation of the current screen geometry, taken by the same session
// over the same connection generation.
func (s *Server) checkObservation(g Grant, id string, pointer bool) error {
	if !pointer && id == "" {
		return nil
	}
	if !pointer && s.obs == nil && s.consumed != nil && s.consumed.id == id {
		c := s.consumed
		if c.session != g.SessionID || c.generation != g.Generation {
			return errors.New("observation belongs to an earlier connection; observe again")
		}
		if time.Since(c.at) > observationTTL {
			return errors.New("observation is older than two minutes; observe again")
		}
		return nil
	}
	if s.obs == nil || s.obs.id != id {
		return errors.New("observation_id is not the latest observation of this desktop; observe again")
	}
	if s.obs.session != g.SessionID || s.obs.generation != g.Generation {
		return errors.New("observation belongs to an earlier connection; observe again")
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
	g := grantFrom(r)
	d := g.Device
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	if replay, err := s.journal.Replay(in.OperationID, d.ID, RequestHash(in), g.SessionID); err != nil {
		writeError(w, http.StatusConflict, "operation_conflict", err.Error(), false, "none")
		return
	} else if replay != nil {
		writeJSON(w, http.StatusOK, replay)
		return
	}
	if err := s.checkObservation(g, in.ObservationID, pointer); err != nil {
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
	op, existing, err := s.begin(r, Operation{ID: in.OperationID, Kind: "desktop_" + in.Action, RequestHash: RequestHash(in)})
	if err != nil {
		beginError(w, err)
		return
	}
	if existing {
		writeJSON(w, http.StatusOK, op)
		return
	}
	ierr := s.sendInput(in)
	final, ferr := s.finish(g, op.ID, func(o *Operation) {
		if ierr != nil {
			o.State, o.Effect, o.Error = taskstate.Failed, "unknown", ierr.Error()
			return
		}
		o.State, o.Effect = taskstate.Completed, "applied"
	})
	if pointer {
		// Pointer input may change the layout; the next pointer action needs
		// a fresh observation. Keyboard input may still use it.
		if s.obs != nil {
			s.consumed = s.obs
		}
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
	areq := computer.AutomationRequest{Action: req.Action, WindowID: req.WindowID, Name: req.Name, Role: req.Role, MaxElements: req.MaxElements, Refresh: req.Refresh}
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

// LaunchRequest starts a GUI program in the node's desktop session. A
// program is named either by literal argv or by App (a catalog app_id or
// name) with literal Args and an optional browser Profile; App is resolved
// by the node's own application catalog, so install paths of one computer
// are never used on another.
type LaunchRequest struct {
	OperationID string   `json:"operation_id"`
	Argv        []string `json:"argv,omitempty"`
	App         string   `json:"app,omitempty"`
	Args        []string `json:"args,omitempty"`
	Profile     string   `json:"profile,omitempty"`
}

// resolveLaunch turns a launch request into literal argv. A bare program
// name that is not on PATH (chrome is usually not) is looked up in the
// application catalog, the way the Run dialog uses App Paths.
func (s *Server) resolveLaunch(ctx context.Context, req LaunchRequest) ([]string, *computer.LaunchPlan, *APIError) {
	launcher, hasCatalog := s.opts.Desktop.(computer.AppLauncherBackend)
	if req.App == "" {
		if len(req.Argv) == 0 || req.Argv[0] == "" || len(req.Argv) > 64 {
			return nil, nil, &APIError{Code: "invalid_request", Message: "argv must contain 1-64 literal arguments, or name app", Effect: "none"}
		}
		if req.Profile != "" || len(req.Args) > 0 {
			return nil, nil, &APIError{Code: "invalid_request", Message: "args and profile need app; pass the whole command line in argv otherwise", Effect: "none"}
		}
		name := req.Argv[0]
		if !hasCatalog || strings.ContainsAny(name, `/\`) {
			return req.Argv, nil, nil
		}
		if _, err := exec.LookPath(name); err == nil {
			return req.Argv, nil, nil
		}
		req.App, req.Args = name, req.Argv[1:]
	} else if len(req.Argv) > 0 {
		return nil, nil, &APIError{Code: "invalid_request", Message: "pass either argv or app, not both", Effect: "none"}
	}
	if !hasCatalog {
		return nil, nil, &APIError{Code: "app_catalog_unavailable", Message: "this computer has no application catalog; pass literal argv", Effect: "none"}
	}
	entry, err := launcher.Apps().Resolve(ctx, req.App)
	if err == nil {
		var plan computer.LaunchPlan
		if plan, err = computer.PlanLaunch(entry, req.Args, req.Profile); err == nil {
			argv := plan.Argv
			if entry.Launch.Method == computer.LaunchAppsFolder {
				argv = append([]string{filepath.Join(os.Getenv("WINDIR"), "explorer.exe")}, argv[1:]...)
			}
			return argv, &plan, nil
		}
	}
	code := "app_not_found"
	for _, c := range []error{computer.ErrAppAmbiguous, computer.ErrAppNotInstalled, computer.ErrProfileNotFound, computer.ErrProfileAmbig, computer.ErrLaunchArgs} {
		if errors.Is(err, c) {
			code = c.Error()
		}
	}
	return nil, nil, &APIError{Code: code, Message: err.Error(), Effect: "none"}
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req LaunchRequest
	if !decode(w, r, &req) {
		return
	}
	g := grantFrom(r)
	if replay, err := s.journal.Replay(req.OperationID, g.Device.ID, RequestHash(req), g.SessionID); err != nil {
		beginError(w, err)
		return
	} else if replay != nil {
		writeJSON(w, http.StatusOK, replay)
		return
	}
	argv, plan, apiErr := s.resolveLaunch(r.Context(), req)
	if apiErr != nil {
		writeJSON(w, http.StatusBadRequest, apiErr)
		return
	}
	// The policy sees the resolved program, so a catalog launch needs the
	// same capabilities as the literal command it becomes.
	if policy, ok := s.opts.Access.(ExecPolicy); ok {
		if apiErr := policy.CheckExec(g, "launch", argv); apiErr != nil {
			writeJSON(w, http.StatusForbidden, apiErr)
			return
		}
	}
	op, existing, err := s.begin(r, Operation{ID: req.OperationID, Kind: "desktop_launch", RequestHash: RequestHash(req), Argv: argv})
	if err != nil {
		beginError(w, err)
		return
	}
	if existing {
		writeJSON(w, http.StatusOK, op)
		return
	}
	dir, _ := s.resolveDir(g, "")
	proc, lerr := procown.Start(procown.Spec{Argv: argv, Dir: dir, Visible: true})
	final, ferr := s.finish(g, op.ID, func(o *Operation) {
		if lerr != nil {
			o.State, o.Effect, o.Error = taskstate.Failed, "none", lerr.Error()
			return
		}
		o.State, o.Effect = taskstate.Completed, "applied"
		o.Stdout = fmt.Sprintf("pid=%d created_at=%d", proc.Identity.PID, proc.Identity.CreatedAt)
		if plan != nil {
			o.Stdout += fmt.Sprintf(" app_id=%s", plan.Entry.ID)
			if plan.Profile != nil {
				o.Stdout += fmt.Sprintf(" profile=%q", plan.Profile.Directory)
			}
		}
	})
	if ferr != nil {
		writeError(w, http.StatusInternalServerError, "persist_failed", ferr.Error(), true, "unknown")
		return
	}
	writeJSON(w, http.StatusOK, final)
}
