// Package dots stores Atlas Dots: long-lived personal agents, each with a
// name, a look, a voice, a personality and permission rules of its own.
//
// A dot is not a new kind of agent. It is a directory under the Atlas data
// directory that Atlas opens as an ordinary workspace: the dot's persona is
// written to the workspace's AGENTS.md, which the coder agent already reads
// as project context, and the dot's sessions, runs and permission requests
// are the server's own. This package owns only what the server does not
// know about: the dot's identity, its look and voice, and when its full
// access mode expires.
package dots

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Limits on user-editable fields.
const (
	maxNameRunes   = 40
	maxRoleRunes   = 60
	maxPromptRunes = 8000
	maxRuleRunes   = 200
	maxRules       = 50
)

// Shapes are the avatar silhouettes the client can draw.
var Shapes = []string{"orb", "pebble", "bloom", "hex", "drop", "cloud", "star", "shield"}

// Colors are the avatar fills the client offers, as CSS hex colors.
var Colors = []string{
	"#1c1c1f", "#a8693c", "#e5483f", "#f2762e", "#f2b632", "#2fb67c",
	"#1fa7a0", "#3b7cf2", "#8d5cf6", "#e2498a", "#8e8e96", "#e9e2d6",
}

// Personality holds the four sliders, each 0-100.
type Personality struct {
	Warmth      int `json:"warmth"`
	Humor       int `json:"humor"`
	Formality   int `json:"formality"`
	Proactivity int `json:"proactivity"`
}

// Rules are the permission rules the user wrote for a dot, in their own
// words. They are given to the agent as instructions.
type Rules struct {
	AlwaysAsk []string `json:"always_ask"`
	AutoAllow []string `json:"auto_allow"`
}

// FullAccess records that the user accepted the full access warning. A
// zero Until with Permanent false means the mode is off.
type FullAccess struct {
	GrantedAt time.Time `json:"granted_at,omitzero"`
	Until     time.Time `json:"until,omitzero"`
	Permanent bool      `json:"permanent,omitempty"`
}

// Active reports whether full access is on at now.
func (f FullAccess) Active(now time.Time) bool {
	if f.GrantedAt.IsZero() {
		return false
	}
	return f.Permanent || now.Before(f.Until)
}

// AccessWindow is one stretch of time a dot had full access, kept so the
// owner can review what the dot did without asking. To is zero while the
// window is open.
type AccessWindow struct {
	From      time.Time `json:"from"`
	To        time.Time `json:"to,omitzero"`
	Planned   time.Time `json:"planned,omitzero"`
	Permanent bool      `json:"permanent,omitempty"`
	// End says how the window closed: revoked, expired or replaced.
	End string `json:"end,omitempty"`
}

// maxAccessLog is how many full access windows a dot remembers.
const maxAccessLog = 50

// Dot is one personal agent.
type Dot struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Role         string      `json:"role"`
	Shape        string      `json:"shape"`
	Color        string      `json:"color"`
	VoiceID      string      `json:"voice_id"`
	VoiceName    string      `json:"voice_name"`
	SystemPrompt string      `json:"system_prompt"`
	Personality  Personality `json:"personality"`
	Rules        Rules       `json:"rules"`
	FullAccess   FullAccess  `json:"full_access"`
	// AccessLog lists past and current full access windows, oldest first.
	AccessLog []AccessWindow `json:"access_log"`
	// Skills names the Atlas skills the owner switched on for this dot.
	Skills    []string  `json:"skills"`
	SessionID string    `json:"session_id"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Input is the editable part of a dot. Nil fields are left unchanged on
// update.
type Input struct {
	Name         *string      `json:"name"`
	Role         *string      `json:"role"`
	Shape        *string      `json:"shape"`
	Color        *string      `json:"color"`
	VoiceID      *string      `json:"voice_id"`
	VoiceName    *string      `json:"voice_name"`
	SystemPrompt *string      `json:"system_prompt"`
	Personality  *Personality `json:"personality"`
	Rules        *Rules       `json:"rules"`
	Skills       *[]string    `json:"skills"`
	SessionID    *string      `json:"session_id"`
}

// ErrNotFound reports an unknown dot ID.
var ErrNotFound = errors.New("dot not found")

// Store keeps dots in one JSON file and their workspaces beside it.
// It is safe for concurrent use.
type Store struct {
	dir string
	now func() time.Time

	mu   sync.Mutex
	dots []Dot
}

// Open loads the store in dir, creating it when missing.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create dots directory: %w", err)
	}
	s := &Store{dir: dir, now: time.Now}
	raw, err := os.ReadFile(s.file())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read dots: %w", err)
	default:
		if err := json.Unmarshal(raw, &s.dots); err != nil {
			return nil, fmt.Errorf("parse dots: %w", err)
		}
	}
	return s, nil
}

func (s *Store) file() string { return filepath.Join(s.dir, "dots.json") }

// List returns every dot, oldest first.
func (s *Store) List() []Dot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.dots)
}

// Get returns one dot.
func (s *Store) Get(id string) (Dot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Dot{}, ErrNotFound
	}
	return s.dots[i], nil
}

// Create validates in, creates the dot's workspace and persona file, and
// saves the dot.
func (s *Store) Create(in Input) (Dot, error) {
	now := s.now()
	d := Dot{
		Shape:       Shapes[0],
		Color:       Colors[1],
		Personality: Personality{Warmth: 60, Humor: 35, Formality: 40, Proactivity: 60},
		CreatedAt:   now,
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return Dot{}, errors.New("a dot needs a name")
	}
	if err := apply(&d, in); err != nil {
		return Dot{}, err
	}
	id, err := newID()
	if err != nil {
		return Dot{}, err
	}
	d.ID = id
	d.Workspace = filepath.Join(s.dir, id)
	d.UpdatedAt = now

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(d.Workspace, 0o700); err != nil {
		return Dot{}, fmt.Errorf("create dot workspace: %w", err)
	}
	if err := writePersona(d, now); err != nil {
		return Dot{}, err
	}
	if err := writeMarker(d); err != nil {
		return Dot{}, err
	}
	s.dots = append(s.dots, d)
	if err := s.save(); err != nil {
		s.dots = s.dots[:len(s.dots)-1]
		return Dot{}, err
	}
	return d, nil
}

// Update applies in to a dot and rewrites its persona file.
func (s *Store) Update(id string, in Input) (Dot, error) {
	return s.mutate(id, func(d *Dot) error { return apply(d, in) })
}

// SetFullAccess turns full access on for d. A zero duration with
// permanent false turns it off.
func (s *Store) SetFullAccess(id string, d time.Duration, permanent bool) (Dot, error) {
	return s.mutate(id, func(dot *Dot) error {
		now := s.now()
		if d <= 0 && !permanent {
			closeWindow(dot, now, "revoked")
			dot.FullAccess = FullAccess{}
			return nil
		}
		closeWindow(dot, now, "replaced")
		dot.FullAccess = FullAccess{GrantedAt: now, Permanent: permanent}
		w := AccessWindow{From: now, Permanent: permanent}
		if !permanent {
			dot.FullAccess.Until = now.Add(d)
			w.Planned = dot.FullAccess.Until
		}
		dot.AccessLog = append(dot.AccessLog, w)
		if n := len(dot.AccessLog); n > maxAccessLog {
			dot.AccessLog = slices.Clone(dot.AccessLog[n-maxAccessLog:])
		}
		return nil
	})
}

// closeWindow ends the dot's open full access window, if any.
func closeWindow(d *Dot, at time.Time, end string) {
	if n := len(d.AccessLog); n > 0 && d.AccessLog[n-1].To.IsZero() {
		d.AccessLog[n-1].To, d.AccessLog[n-1].End = at, end
	}
}

// ExpireFullAccess turns off full access that has run out and rewrites
// those dots' persona files, so the agent stops acting without approval
// as soon as the time is up.
func (s *Store) ExpireFullAccess() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	changed := false
	for i := range s.dots {
		f := s.dots[i].FullAccess
		if f.GrantedAt.IsZero() || f.Active(now) {
			continue
		}
		closeWindow(&s.dots[i], f.Until, "expired")
		s.dots[i].FullAccess = FullAccess{}
		s.dots[i].UpdatedAt = now
		if err := writePersona(s.dots[i], now); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return s.save()
}

// Delete removes a dot from the list. Its workspace stays on disk, so its
// files and history are never lost by a click.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}
	removed := s.dots[i]
	s.dots = slices.Delete(s.dots, i, i+1)
	if err := s.save(); err != nil {
		s.dots = slices.Insert(s.dots, i, removed)
		return err
	}
	s.mu.Unlock()
	_ = Stop(removed.Workspace)
	s.mu.Lock()
	return nil
}

func (s *Store) mutate(id string, fn func(*Dot) error) (Dot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Dot{}, ErrNotFound
	}
	d := s.dots[i]
	if err := fn(&d); err != nil {
		return Dot{}, err
	}
	now := s.now()
	d.UpdatedAt = now
	if err := writePersona(d, now); err != nil {
		return Dot{}, err
	}
	prev := s.dots[i]
	s.dots[i] = d
	if err := s.save(); err != nil {
		s.dots[i] = prev
		return Dot{}, err
	}
	return d, nil
}

func (s *Store) index(id string) int {
	return slices.IndexFunc(s.dots, func(d Dot) bool { return d.ID == id })
}

// save writes the list atomically. The caller holds s.mu.
func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.dots, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(s.file(), raw); err != nil {
		return fmt.Errorf("write dots: %w", err)
	}
	return nil
}

func apply(d *Dot, in Input) error {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || utf8.RuneCountInString(name) > maxNameRunes {
			return fmt.Errorf("name must be 1-%d characters", maxNameRunes)
		}
		d.Name = name
	}
	if in.Role != nil {
		role := strings.TrimSpace(*in.Role)
		if utf8.RuneCountInString(role) > maxRoleRunes {
			return fmt.Errorf("role must be at most %d characters", maxRoleRunes)
		}
		d.Role = role
	}
	if in.Shape != nil {
		if !slices.Contains(Shapes, *in.Shape) {
			return errors.New("unknown shape")
		}
		d.Shape = *in.Shape
	}
	if in.Color != nil {
		if !slices.Contains(Colors, *in.Color) {
			return errors.New("unknown color")
		}
		d.Color = *in.Color
	}
	if in.VoiceID != nil {
		d.VoiceID = strings.TrimSpace(*in.VoiceID)
	}
	if in.VoiceName != nil {
		d.VoiceName = strings.TrimSpace(*in.VoiceName)
	}
	if in.SystemPrompt != nil {
		if utf8.RuneCountInString(*in.SystemPrompt) > maxPromptRunes {
			return fmt.Errorf("system prompt must be at most %d characters", maxPromptRunes)
		}
		d.SystemPrompt = strings.TrimSpace(*in.SystemPrompt)
	}
	if in.Personality != nil {
		p := *in.Personality
		for _, v := range []int{p.Warmth, p.Humor, p.Formality, p.Proactivity} {
			if v < 0 || v > 100 {
				return errors.New("personality values must be 0-100")
			}
		}
		d.Personality = p
	}
	if in.Rules != nil {
		ask, err := cleanRules(in.Rules.AlwaysAsk)
		if err != nil {
			return err
		}
		allow, err := cleanRules(in.Rules.AutoAllow)
		if err != nil {
			return err
		}
		d.Rules = Rules{AlwaysAsk: ask, AutoAllow: allow}
	}
	if in.Skills != nil {
		skills := make([]string, 0, len(*in.Skills))
		for _, name := range *in.Skills {
			name = strings.TrimSpace(name)
			if name == "" || slices.Contains(skills, name) {
				continue
			}
			if utf8.RuneCountInString(name) > maxRuleRunes {
				return errors.New("skill name is too long")
			}
			skills = append(skills, name)
		}
		if len(skills) > maxRules {
			return fmt.Errorf("at most %d skills", maxRules)
		}
		d.Skills = skills
	}
	if in.SessionID != nil {
		d.SessionID = strings.TrimSpace(*in.SessionID)
	}
	return nil
}

func cleanRules(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if utf8.RuneCountInString(r) > maxRuleRunes {
			return nil, fmt.Errorf("a rule must be at most %d characters", maxRuleRunes)
		}
		out = append(out, r)
	}
	if len(out) > maxRules {
		return nil, fmt.Errorf("at most %d rules", maxRules)
	}
	return out, nil
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "dot_" + hex.EncodeToString(b[:]), nil
}
