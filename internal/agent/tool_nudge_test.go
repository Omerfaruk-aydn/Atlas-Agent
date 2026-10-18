package agent

import (
	"context"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func echoBashTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(tools.BashToolName, "Run a command.", func(context.Context, tools.BashParams, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse("output"), nil
	})
}

func runNudged(t *testing.T, palette []fantasy.AgentTool, sessionID, command string) string {
	t.Helper()
	bash := palette[0]
	require.Equal(t, tools.BashToolName, bash.Info().Name)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sessionID)
	resp, err := bash.Run(ctx, fantasy.ToolCall{Input: `{"command":` + jsonString(command) + `,"description":"d"}`})
	require.NoError(t, err)
	return resp.Content
}

func jsonString(s string) string {
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		default:
			out = append(out, string(r)...)
		}
	}
	return string(append(out, '"'))
}

func TestShellNudgeNamesTheBuiltToolOncePerSession(t *testing.T) {
	t.Parallel()

	authorized := fixturePalette([]string{"bash", "browser", "fetch", "computer"})
	given := csync.NewMap[string, bool]()
	palette := nudgeShellTools([]fantasy.AgentTool{echoBashTool()}, authorized, given, nil)

	first := runNudged(t, palette, "s1", "npx playwright screenshot http://localhost:3000 shot.png")
	require.Contains(t, first, "output")
	require.Contains(t, first, "the `browser` tool")
	require.Contains(t, first, `{"names":["browser"]}`)

	again := runNudged(t, palette, "s1", "node shot.js # uses puppeteer")
	require.Equal(t, "output", again, "said once per session")

	other := runNudged(t, palette, "s2", "python -m selenium_run")
	require.Equal(t, "output", other, "selenium_run is not the selenium word")
	require.Contains(t, runNudged(t, palette, "s2", "python drive.py --selenium && selenium --version"), "`browser`")
}

func TestShellNudgeRecognizesHabits(t *testing.T) {
	t.Parallel()

	authorized := fixturePalette([]string{"bash", "browser", "fetch", "computer", "android", "debugger"})
	cases := map[string]string{
		"curl -s https://example.com/docs":                    "fetch",
		"Invoke-WebRequest https://example.com | Select -Exp": "fetch",
		"chrome.exe --headless --screenshot=a.png file.html":  "browser",
		"xdotool search --name Notepad":                       "computer",
		"adb shell input tap 10 10":                           "android",
		"dlv debug ./cmd/app":                                 "debugger",
	}
	for command, tool := range cases {
		palette := nudgeShellTools([]fantasy.AgentTool{echoBashTool()}, authorized, csync.NewMap[string, bool](), nil)
		require.Contains(t, runNudged(t, palette, "s", command), "`"+tool+"`", command)
	}
}

func TestShellNudgeLeavesLegitimateShellWorkAlone(t *testing.T) {
	t.Parallel()

	authorized := fixturePalette([]string{"bash", "browser", "fetch", "computer"})
	for _, command := range []string{
		"curl -s http://localhost:8080/health",
		"curl -o tool.tar.gz https://example.com/tool.tar.gz",
		"curl -X POST https://api.example.com/v1/items -d '{}'",
		"go test ./...",
		"grep -r playwrightConfig src",
	} {
		palette := nudgeShellTools([]fantasy.AgentTool{echoBashTool()}, authorized, csync.NewMap[string, bool](), nil)
		require.Equal(t, "output", runNudged(t, palette, "s", command), command)
	}
}

func TestShellNudgeOnlyNamesAuthorizedTools(t *testing.T) {
	t.Parallel()

	bash := echoBashTool()
	palette := nudgeShellTools([]fantasy.AgentTool{bash}, fixturePalette([]string{"bash"}), csync.NewMap[string, bool](), nil)
	require.Same(t, bash, palette[0], "nothing to suggest leaves bash unwrapped")

	without := nudgeShellTools([]fantasy.AgentTool{fixturePalette([]string{"view"})[0]}, fixturePalette([]string{"browser"}), csync.NewMap[string, bool](), nil)
	require.Equal(t, "view", without[0].Info().Name, "no bash, no change")
}
