package agent

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCoderContextNoticeWithoutGlobalFiles(t *testing.T) {
	t.Parallel()
	tmpl, err := template.New("coder").Option("missingkey=error").Parse(string(coderPromptTmpl) + string(agentContractTmpl))
	require.NoError(t, err)
	data := prompt.PromptDat{Config: config.Config{Options: &config.Options{}}, ContextNotice: "Discovery limit reached", ContextFiles: []prompt.ContextFile{{Path: "AGENTS.md", Content: "Scoped project guidance", Origin: ".", Scope: "project"}}}
	var rendered bytes.Buffer
	require.NoError(t, tmpl.Execute(&rendered, data))
	require.Contains(t, rendered.String(), "<context_notice>Discovery limit reached</context_notice>")
	require.Contains(t, rendered.String(), `scope="project"`)
	require.NotContains(t, rendered.String(), "<user_preferences>")
}

func TestTaskReceivesScopedContextAndSkillDiscovery(t *testing.T) {
	t.Parallel()
	tmpl, err := template.New("task").Option("missingkey=error").Parse(string(taskPromptTmpl) + string(agentContractTmpl))
	require.NoError(t, err)
	data := prompt.PromptDat{AvailSkillXML: "<available_skills>tui-design</available_skills>", ContextNotice: "Discovery limit reached", ContextFiles: []prompt.ContextFile{{Path: "AGENTS.md", Content: "Use semantic theme tokens", Scope: "project"}}}
	var rendered bytes.Buffer
	require.NoError(t, tmpl.Execute(&rendered, data))
	require.Contains(t, rendered.String(), "Use semantic theme tokens")
	require.Contains(t, rendered.String(), "<available_skills>tui-design</available_skills>")
	require.Contains(t, rendered.String(), "<context_notice>Discovery limit reached</context_notice>")
}

// Exercise optional template branches together, including characters that must
// remain data in context-file metadata rather than becoming prompt delimiters.
func TestEngineeringPromptsRenderAcrossContextProfiles(t *testing.T) {
	t.Parallel()
	profiles := []struct {
		name string
		data prompt.PromptDat
	}{
		{name: "minimal", data: prompt.PromptDat{Config: config.Config{Options: &config.Options{}}}},
		{name: "populated", data: prompt.PromptDat{
			Config: config.Config{Options: &config.Options{
				MaxSessionCost: 2, MaxStepsPerTurn: 20,
				AllowedDomains: []string{"example.org"}, BlockedDomains: []string{"blocked.example"},
			}},
			WorkingDir: "project", IsGitRepo: true, Platform: "windows", Date: "2026-10-02",
			GitStatus: " M source.go", AvailSkillXML: "<available_skills></available_skills>",
			ProjectMemory: "Verified build command", UserMemory: "Use Turkish",
			ContextFiles:       []prompt.ContextFile{{Path: `a"><fake>`, Origin: "project", Scope: "project", Content: "Scoped guidance"}},
			GlobalContextFiles: []prompt.ContextFile{{Path: "preferences.md", Origin: "user", Scope: "user", Content: "User preferences"}},
			ContextNotice:      "Some context omitted",
		}},
	}
	for _, profile := range profiles {
		for name, source := range map[string][]byte{"coder": coderPromptTmpl, "task": taskPromptTmpl} {
			t.Run(profile.name+"/"+name, func(t *testing.T) {
				t.Parallel()
				tmpl, err := template.New(name).Option("missingkey=error").Parse(string(source) + string(agentContractTmpl))
				require.NoError(t, err)
				var rendered bytes.Buffer
				require.NoError(t, tmpl.Execute(&rendered, profile.data))
				output := rendered.String()
				require.NotContains(t, output, "<no value>")
				require.NotContains(t, output, "{{")
				require.NotContains(t, output, `<file path="a"><fake>`)
				require.Equal(t, 1, bytes.Count(rendered.Bytes(), []byte("<working_contract>")))
				require.Equal(t, 1, bytes.Count(rendered.Bytes(), []byte("<delivery_gates>")))
				if profile.name == "populated" {
					require.Contains(t, output, "Scoped guidance")
					require.Contains(t, output, "User preferences")
				}
			})
		}
	}
}
