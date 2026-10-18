package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
)

func (c *coordinator) expertiseWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	id := engineering.GetScope(ctx, tools.GetSessionFromContext(ctx)).SessionID
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	st, err := c.engineering.Read(ctx, id)
	if err != nil {
		return fail(err)
	}
	if p.ExpertiseAction == "list" || p.ExpertiseAction == "" {
		var available []map[string]string
		for _, s := range c.activeSkills {
			if !s.DisableModelInvocation {
				available = append(available, map[string]string{"name": s.Name, "description": s.Description, "exclusive_group": s.Metadata["exclusive-group"], "conflicts": s.Metadata["conflicts"]})
			}
		}
		return workResponse(map[string]any{"available": available, "loaded": st.Expertise, "precedence": "Project/user instructions and permissions override all guide content. If guidance contradicts current requirements, unload it rather than silently combine it."})
	}
	if p.ExpertiseAction == "unload" {
		err := c.engineering.Update(ctx, id, func(st *engineering.State) error {
			found := false
			for i := range st.Expertise {
				if st.Expertise[i].Name == p.ExpertiseName {
					st.Expertise[i].Active = false
					found = true
				}
			}
			if !found {
				return fmt.Errorf("guide was not loaded through expertise")
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
		c.skillTracker.MarkUnloaded(p.ExpertiseName)
		return workResponse(map[string]any{"unloaded": p.ExpertiseName, "history": "Original audit records remain; guide bodies are omitted from subsequent model requests."})
	}
	if p.ExpertiseAction != "load" {
		return fail(fmt.Errorf("expertise_action must be list, load or unload"))
	}
	s, ok := skills.Find(c.activeSkills, p.ExpertiseName)
	if !ok || s.DisableModelInvocation {
		return fail(fmt.Errorf("skill is unavailable for model invocation"))
	}
	group := s.Metadata["exclusive-group"]
	for _, active := range st.Expertise {
		if !active.Active || active.Name == s.Name {
			continue
		}
		other, _ := skills.Find(c.activeSkills, active.Name)
		conflict := expertiseConflicts(s.Metadata["conflicts"], active.Name)
		if other != nil {
			conflict = conflict || expertiseConflicts(other.Metadata["conflicts"], s.Name)
		}
		if (group != "" && active.Group == group) || conflict {
			return fail(fmt.Errorf("guide conflicts with loaded %s; unload it before changing guidance", active.Name))
		}
	}
	if len(st.Expertise) >= 32 {
		return fail(fmt.Errorf("expertise load history full"))
	}
	if invoke == nil {
		return fail(fmt.Errorf("view invocation unavailable"))
	}
	path := s.SkillFilePath
	if path == "" {
		path = skills.SkillPath(s.Path, "")
	}
	response, err := invoke(ctx, fantasy.ToolCall{ID: call.ID + "-guide", Name: tools.ViewToolName, Input: workflowJSON(tools.ViewParams{FilePath: path})})
	if err != nil || response.IsError || response.StopTurn {
		return response, err
	}
	var meta tools.ViewResponseMetadata
	if err := json.Unmarshal([]byte(response.Metadata), &meta); err != nil || meta.ResourceName != s.Name || meta.ResourceType != tools.ViewResourceSkill {
		return fail(fmt.Errorf("loaded resource did not match the selected skill"))
	}
	err = c.engineering.Update(ctx, id, func(st *engineering.State) error {
		for i := range st.Expertise {
			if st.Expertise[i].Name == s.Name {
				st.Expertise[i].Active = false
			}
		}
		st.Expertise = append(st.Expertise, engineering.ExpertiseRecord{Name: s.Name, Source: path, Hash: engineering.Hash(meta.Content), Group: group, CallID: call.ID, Active: true})
		return nil
	})
	if err != nil {
		return fail(err)
	}
	return response, nil
}

func expertiseConflicts(list, name string) bool {
	return slices.ContainsFunc(strings.Split(list, ","), func(v string) bool { return strings.EqualFold(strings.TrimSpace(v), name) })
}

// withExpertiseLifecycle omits unloaded guide bodies but keeps tool-call pairing.
// The original stored messages remain intact for audit and reloading.
func withExpertiseLifecycle(messages []fantasy.Message, st engineering.State) []fantasy.Message {
	released := map[string]bool{}
	for _, guide := range st.Expertise {
		if !guide.Active {
			released[guide.CallID] = true
		}
	}
	if len(released) == 0 {
		return messages
	}
	out := slices.Clone(messages)
	for i, msg := range out {
		out[i].Content = slices.Clone(msg.Content)
		for j, part := range msg.Content {
			result, ok := part.(fantasy.ToolResultPart)
			if !ok || !released[result.ToolCallID] {
				continue
			}
			result.Output = fantasy.ToolResultOutputContentText{Text: "Guide unloaded. Original content remains in the audit history; reload explicitly if needed."}
			result.ClientMetadata = ""
			out[i].Content[j] = result
		}
	}
	return out
}
