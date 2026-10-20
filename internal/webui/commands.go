package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
)

// slashArgument is one placeholder a slash command needs a value for.
type slashArgument struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// slashCommand is an entry of the composer's "/" menu: the same custom
// commands, workflow recipes and user-invocable skills the terminal UI
// offers in its command palette.
type slashCommand struct {
	// ID identifies the command to the expand endpoint.
	ID string `json:"id"`
	// Name is what the reader types after the slash.
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Kind        string          `json:"kind"` // "custom", "workflow" or "skill"
	Arguments   []slashArgument `json:"arguments,omitempty"`
}

func toSlashArguments(args []commands.Argument) []slashArgument {
	out := make([]slashArgument, 0, len(args))
	for _, a := range args {
		out = append(out, slashArgument{ID: a.ID, Title: a.Title, Description: a.Description, Required: a.Required})
	}
	return out
}

// projectCommands loads the custom commands and recipes that apply to the
// workspace directory: the markdown commands of the user's and the
// project's commands folders, the built-in quality flows and workflow
// recipes.
func projectCommands(ctx context.Context, dir string) ([]commands.CustomCommand, error) {
	cfg := &config.Config{Options: &config.Options{DataDirectory: filepath.Join(dir, ".atlas")}}
	loaded, err := commands.LoadCustomCommands(cfg)
	if err != nil {
		return nil, err
	}
	recipes, err := workflows.Load(ctx, workflows.Paths(dir, nil))
	if err != nil {
		// Recipes are optional; the rest of the menu still works.
		return loaded, nil
	}
	combined, err := commands.AppendRecipes(loaded, recipes)
	if err != nil {
		return loaded, nil
	}
	return combined, nil
}

// userSkills lists the skills the user may invoke by name.
func (g *Gateway) userSkills(ctx context.Context, h *hub) []proto.SkillInfo {
	rsp, err := g.opts.Upstream.Do(ctx, http.MethodGet, "/workspaces/"+url.PathEscape(h.workspaceID())+"/skills", nil, nil, nil)
	if err != nil {
		return nil
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		return nil
	}
	var skills []proto.SkillInfo
	if json.NewDecoder(rsp.Body).Decode(&skills) != nil {
		return nil
	}
	out := skills[:0]
	for _, s := range skills {
		if s.UserInvocable {
			out = append(out, s)
		}
	}
	return out
}

func skillSlashName(s proto.SkillInfo) string {
	if s.Label != "" {
		return s.Label
	}
	return "user:" + s.Name
}

func (g *Gateway) handleListCommands(w http.ResponseWriter, r *http.Request, h *hub) {
	ctx := r.Context()
	custom, err := projectCommands(ctx, h.path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load commands")
		return
	}
	out := make([]slashCommand, 0, len(custom))
	for _, c := range custom {
		kind := "custom"
		if c.Recipe != nil {
			kind = "workflow"
		}
		out = append(out, slashCommand{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Kind: kind, Arguments: toSlashArguments(c.Arguments)})
	}
	for _, s := range g.userSkills(ctx, h) {
		out = append(out, slashCommand{ID: "skill:" + s.Name, Name: strings.TrimPrefix(skillSlashName(s), "/"), Description: s.Description, Kind: "skill"})
	}
	writeJSON(w, http.StatusOK, out)
}

type expandRequest struct {
	ID   string            `json:"id"`
	Args map[string]string `json:"args,omitempty"`
}

// handleExpandCommand turns a chosen command and its argument values into
// the prompt text the browser sends, the way the terminal UI does when a
// command is run.
func (g *Gateway) handleExpandCommand(w http.ResponseWriter, r *http.Request, h *hub) {
	var req expandRequest
	if !decodeJSON(w, r, 64<<10, &req) {
		return
	}
	if name, ok := strings.CutPrefix(req.ID, "skill:"); ok {
		writeJSON(w, http.StatusOK, map[string]string{"prompt": "Use the \"" + name + "\" skill for this task."})
		return
	}
	custom, err := projectCommands(r.Context(), h.path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load commands")
		return
	}
	for _, c := range custom {
		if c.ID != req.ID {
			continue
		}
		if c.Recipe != nil {
			prompt, err := commands.RecipeInvocation(*c.Recipe, req.Args)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt})
			return
		}
		prompt := c.Content
		for name, value := range req.Args {
			prompt = strings.ReplaceAll(prompt, "$"+name, value)
		}
		writeJSON(w, http.StatusOK, map[string]string{"prompt": prompt})
		return
	}
	writeError(w, http.StatusNotFound, "unknown command")
}
