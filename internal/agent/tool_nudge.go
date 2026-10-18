package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
)

// A model that does not know a tool exists takes the long way round: it
// writes a Playwright script to look at a page, curls HTML to read it, or
// drives the desktop with xdotool. The shell nudge notices such a command
// and adds one line to its result naming the tool built for the job. It
// never blocks, rewrites or delays the command, it only names tools the
// session is authorized to use, and it says so at most once per tool per
// session, so a model that has a reason to stay in the shell is not nagged.

// shellNudge maps a shell habit to the tool that does the same job.
type shellNudge struct {
	tool    string
	pattern *regexp.Regexp
	// skip excludes commands that look alike but are legitimately shell
	// work, such as probing a local server with curl.
	skip *regexp.Regexp
	why  string
}

var shellNudges = []shellNudge{
	{
		tool:    tools.BrowserToolName,
		pattern: regexp.MustCompile(`(?i)\b(playwright|puppeteer|selenium|webdriver|wkhtmltoimage)\b|\b(chrome|chromium|msedge)(\.exe)?\b[^|;&]*--(headless|screenshot)`),
		why:     "drives a real page directly: navigate, click, type, read the console and take screenshots, with no script or test suite to write; use it instead of Playwright, Puppeteer or Selenium to check a web UI",
	},
	{
		tool:    tools.FetchToolName,
		pattern: regexp.MustCompile(`(?i)\b(curl|wget|invoke-webrequest|iwr)\b[^|;&]*https?://`),
		skip:    regexp.MustCompile(`(?i)https?://(localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0)|\s(-o|-O|--output|-OutFile)\b|\s-X\s*(POST|PUT|PATCH|DELETE)|\s(-d|--data)\b`),
		why:     "reads a URL as clean text or markdown instead of raw HTML",
	},
	{
		tool:    tools.ComputerToolName,
		pattern: regexp.MustCompile(`(?i)\b(xdotool|pyautogui|nircmd|autohotkey|sendkeys|wmctrl)\b`),
		why:     "sees and controls desktop applications with screenshots, clicks and keystrokes",
	},
	{
		tool:    "android",
		pattern: regexp.MustCompile(`(?i)(^|[\s;&|])adb\s+(shell|install|exec-out|push|pull)\b`),
		why:     "operates Android devices and emulators with screenshots and taps",
	},
	{
		tool:    "debugger",
		pattern: regexp.MustCompile(`(?i)(^|[\s;&|])dlv\s+(debug|test|exec|attach)\b`),
		why:     "runs Delve with breakpoints and live variables without an interactive terminal",
	},
}

// shellObserver is told, after each bash call, which tool the result named
// (empty for none) and whether the output was saved to a log file.
type shellObserver func(sessionID, nudged string, spilled bool)

// nudgeShellTools wraps the bash tool in palette so its results can name a
// better-suited tool. authorized is the session's full tool set, including
// deferred tools: those are exactly the ones a model may not know about.
// observe may be nil.
func nudgeShellTools(palette, authorized []fantasy.AgentTool, given *csync.Map[string, bool], observe shellObserver) []fantasy.AgentTool {
	i := slices.IndexFunc(palette, func(t fantasy.AgentTool) bool { return t.Info().Name == tools.BashToolName })
	if i < 0 || given == nil {
		return palette
	}
	var available []shellNudge
	for _, nudge := range shellNudges {
		if slices.ContainsFunc(authorized, func(t fantasy.AgentTool) bool { return t.Info().Name == nudge.tool }) {
			available = append(available, nudge)
		}
	}
	if len(available) == 0 && observe == nil {
		return palette
	}
	out := slices.Clone(palette)
	out[i] = &nudgedShellTool{AgentTool: palette[i], nudges: available, given: given, observe: observe}
	return out
}

type nudgedShellTool struct {
	fantasy.AgentTool
	nudges  []shellNudge
	given   *csync.Map[string, bool]
	observe shellObserver
}

func (t *nudgedShellTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	resp, err := t.AgentTool.Run(ctx, call)
	if err != nil {
		return resp, err
	}
	var params tools.BashParams
	if json.Unmarshal([]byte(call.Input), &params) != nil {
		return resp, nil
	}
	command := params.Command
	if command == "" {
		command = strings.Join(params.Argv, " ")
	}
	sessionID := tools.GetSessionFromContext(ctx)
	note, nudged := t.note(sessionID, command)
	resp.Content += note
	if t.observe != nil {
		t.observe(sessionID, nudged, strings.Contains(resp.Content, tools.SpilledOutputMarker))
	}
	return resp, nil
}

// note returns the hint for the first matching habit whose tool has not
// been suggested in this session yet, and that tool's name.
func (t *nudgedShellTool) note(sessionID, command string) (string, string) {
	for _, nudge := range t.nudges {
		if !nudge.pattern.MatchString(command) || (nudge.skip != nil && nudge.skip.MatchString(command)) {
			continue
		}
		key := nudgeKey(sessionID, nudge.tool)
		if _, done := t.given.Get(key); done {
			continue
		}
		t.given.Set(key, false)
		return "\n\nNote: the `" + nudge.tool + "` tool " + nudge.why + ". It is enabled for this session; load it with tool_search " +
			`{"names":["` + nudge.tool + `"]}` + " if it fits. Staying in the shell is fine when you need a script.", nudge.tool
	}
	return "", ""
}

func nudgeKey(sessionID, tool string) string { return sessionID + "\x00" + tool }

// recordNudgesFollowed records, once per nudge, that the model went on to
// call the tool a shell nudge named. given holds false for a nudge not yet
// followed and true once it has been.
func (a *sessionAgent) recordNudgesFollowed(sessionID string, steps []fantasy.StepResult) {
	if a.shellNudgesGiven == nil {
		return
	}
	for _, step := range steps {
		for _, call := range step.Content.ToolCalls() {
			key := nudgeKey(sessionID, call.ToolName)
			if followed, ok := a.shellNudgesGiven.Get(key); ok && !followed {
				a.shellNudgesGiven.Set(key, true)
				a.recordHarness(harnessmetrics.NudgeFollowed, sessionID, map[string]string{"tool": call.ToolName})
			}
		}
	}
}

// observeShell records nudges and saved outputs for the harness report.
func (a *sessionAgent) observeShell(sessionID, nudged string, spilled bool) {
	if nudged != "" {
		a.recordHarness(harnessmetrics.NudgeGiven, sessionID, map[string]string{"tool": nudged})
	}
	if spilled {
		a.recordHarness(harnessmetrics.OutputSpilled, sessionID, nil)
	}
}
