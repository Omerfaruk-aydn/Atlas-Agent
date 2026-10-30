package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"text/template"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/prompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func promptBudgetSamples(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	protocols, err := prompt.RenderProtocols(nil)
	require.NoError(t, err)
	data := prompt.PromptDat{Config: config.Config{Options: &config.Options{}}, WorkingDir: "project", Platform: "windows", Date: "10/27/2026", TaskProtocols: protocols}
	for mode, source := range map[string][]byte{"coder": coderPromptTmpl, "task": taskPromptTmpl} {
		tmpl, err := template.New(mode).Parse(string(source) + string(agentContractTmpl))
		require.NoError(t, err)
		var body bytes.Buffer
		require.NoError(t, tmpl.Execute(&body, data))
		for name, palette := range map[string][]string{"none": nil, "browser": {"browser"}, "desktop": {"computer"}, "both": {"browser", "computer"}} {
			messages := withToolProtocols([]fantasy.Message{fantasy.NewSystemMessage(body.String())}, palette)
			var count int64
			for _, msg := range messages {
				for _, part := range msg.Content {
					if text, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
						count += approxTokenCount(strings.ReplaceAll(text.Text, "\r\n", "\n"))
					}
				}
			}
			out[mode+"/"+name] = count
		}
	}
	return out
}

func TestPromptTokenBudgetGolden(t *testing.T) {
	t.Parallel()
	actual := promptBudgetSamples(t)
	if os.Getenv("ATLAS_UPDATE_PROMPT_GOLDEN") == "1" {
		data, err := json.MarshalIndent(actual, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile("testdata/prompt_tokens.golden.json", append(data, '\n'), 0o644))
		return
	}
	data, err := os.ReadFile("testdata/prompt_tokens.golden.json")
	require.NoError(t, err)
	var golden map[string]int64
	require.NoError(t, json.Unmarshal(data, &golden))
	require.Len(t, golden, len(actual))
	for name, count := range actual {
		baseline, ok := golden[name]
		require.True(t, ok, name)
		require.NoError(t, checkPromptBudget(count, baseline), "%s: update the golden only after reviewing the prompt change", name)
	}
	require.NoError(t, checkPromptBudget(105, 100))
	require.Error(t, checkPromptBudget(106, 100))
	require.Error(t, checkPromptBudget(1, 0))
}

func checkPromptBudget(actual, baseline int64) error {
	if baseline <= 0 || actual < 0 || actual > baseline+baseline/20 {
		return fmt.Errorf("prompt budget increased above 5%%: baseline=%d actual=%d (UTF-8 bytes/4 estimate)", baseline, actual)
	}
	return nil
}
