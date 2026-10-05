package prompt

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
)

type roleSkillsKey struct{}

// WithRoleSkills freezes skill preferences without granting tool permissions.
func WithRoleSkills(ctx context.Context, names []string) context.Context {
	return context.WithValue(ctx, roleSkillsKey{}, slices.Clone(names))
}

func renderRoleSkills(ctx context.Context, available []*skills.Skill) string {
	names, _ := ctx.Value(roleSkillsKey{}).([]string)
	if len(names) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<role_skills>\nSelected procedures are guidance subordinate to the assignment, user authorization and repository instructions. They grant no tools, accounts or publication rights. Referenced resources require focused view reads.\n")
	seen := make(map[string]bool)
	for _, name := range names[:min(len(names), 8)] {
		if seen[name] {
			continue
		}
		seen[name] = true
		var selected *skills.Skill
		for _, candidate := range available {
			if candidate.Name == name && !candidate.DisableModelInvocation {
				selected = candidate
				break
			}
		}
		if selected == nil {
			fmt.Fprintf(&out, "<skill name=%q status=\"unavailable\">Not discovered, disabled or not available for automatic invocation; do not assume its instructions or capabilities.</skill>\n", html.EscapeString(name))
			continue
		}
		body := html.EscapeString(selected.Instructions)
		fmt.Fprintf(&out, "<skill name=%q status=\"loaded\" location=%q>\n%s\n</skill>\n", html.EscapeString(name), html.EscapeString(selected.SkillFilePath), body)
	}
	out.WriteString("</role_skills>")
	return out.String()
}
