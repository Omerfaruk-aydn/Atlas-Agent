package webui

import (
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/memory"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// registerDotsExtras adds a dot's memory, its skill choices and its
// spending history.
func (g *Gateway) registerDotsExtras(api *http.ServeMux) {
	if g.opts.Dots == nil {
		return
	}
	api.HandleFunc("GET /api/dots/{dot}/memory", g.dotRoute(g.handleGetMemory))
	api.HandleFunc("PUT /api/dots/{dot}/memory", g.dotRoute(g.handleSetMemory))
	api.HandleFunc("GET /api/dots/{dot}/skills", g.dotRoute(g.handleListSkills))
	if g.opts.Automation != nil {
		api.HandleFunc("GET /api/dots/{dot}/spending", g.dotRoute(g.handleSpending))
	}
}

type dotMemory struct {
	Content string `json:"content"`
	Used    int    `json:"used"`
	Limit   int    `json:"limit"`
}

// dotMemoryStore is the memory Atlas shows the dot at the start of each
// conversation: the project memory of its workspace.
func dotMemoryStore(d dots.Dot) *memory.Store {
	return memory.New(memory.Options{ProjectDir: filepath.Join(d.Workspace, ".atlas", "memory")})
}

func readDotMemory(d dots.Dot) (dotMemory, error) {
	s := dotMemoryStore(d)
	content, err := s.Read(memory.ScopeProject)
	if err != nil {
		return dotMemory{}, err
	}
	used, limit, err := s.Used(memory.ScopeProject)
	if err != nil {
		return dotMemory{}, err
	}
	return dotMemory{Content: content, Used: used, Limit: limit}, nil
}

func (g *Gateway) handleGetMemory(w http.ResponseWriter, _ *http.Request, d dots.Dot) {
	m, err := readDotMemory(d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the dot's memory")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (g *Gateway) handleSetMemory(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	var body struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, maxJSONBody, &body) {
		return
	}
	if _, err := dotMemoryStore(d).Set(memory.ScopeProject, body.Content); err != nil {
		var long *memory.ErrTooLong
		if errors.As(err, &long) {
			writeError(w, http.StatusBadRequest, "Hafıza "+strconv.Itoa(long.Size)+" karakter olur, sınır "+strconv.Itoa(long.Limit)+". Biraz kısalt.")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not save the dot's memory")
		return
	}
	m, err := readDotMemory(d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the dot's memory")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type dotSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Enabled     bool   `json:"enabled"`
}

// handleListSkills lists every Atlas skill the dot's workspace can load,
// marking the ones the owner switched on.
func (g *Gateway) handleListSkills(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	h, err := g.openWorkspace(r.Context(), d.Workspace)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	var skills []proto.SkillInfo
	if err := g.getJSON(r.Context(), "/workspaces/"+url.PathEscape(h.workspaceID())+"/skills", nil, &skills); err != nil {
		writeUpstreamError(w, err)
		return
	}
	on := map[string]bool{}
	for _, s := range d.Skills {
		on[s] = true
	}
	out := make([]dotSkill, 0, len(skills))
	for _, s := range skills {
		out = append(out, dotSkill{Name: s.Name, Description: s.Description, Source: s.Source, Enabled: on[s.Name]})
	}
	writeJSON(w, http.StatusOK, out)
}

type dotSpending struct {
	Days    []dots.DaySpend `json:"days"`
	Budget  dots.Budget     `json:"budget"`
	RunCost float64         `json:"run_cost"`
	Runs    int             `json:"runs"`
}

// handleSpending reports what a dot spent per day over the last two
// weeks, and what its wake-ups cost.
func (g *Gateway) handleSpending(w http.ResponseWriter, r *http.Request, d dots.Dot) {
	days := 14
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n >= 7 && n <= 60 {
		days = n
	}
	budget := g.dotBudget(r.Context(), d)
	out := dotSpending{Days: g.opts.Automation.History(d.ID, days), Budget: budget}
	for _, run := range g.opts.Automation.RunsFor(d.ID, 0) {
		out.RunCost += run.Cost
		out.Runs++
	}
	writeJSON(w, http.StatusOK, out)
}
