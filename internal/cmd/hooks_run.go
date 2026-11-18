package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/spf13/cobra"
)

var (
	hooksRunTool     string
	hooksRunInput    string
	hooksRunResponse string
	hooksRunPrompt   string
)

var hooksRunCmd = &cobra.Command{
	Use:   "run <event>",
	Short: "Run the hooks configured for an event",
	Long: "Actually execute the hooks configured for an event and print what they decided, so a hook can be " +
		"debugged without waiting for a real turn to trigger it. Events: PreToolUse, PostToolUse, " +
		"UserPromptSubmit, SessionStart, SessionEnd, PreCompact, Stop, SubagentStop (snake_case accepted). " +
		"This runs the configured shell commands.",
	Args: cobra.ExactArgs(1),
	RunE: runHooksRun,
}

func init() {
	hooksRunCmd.Flags().StringVar(&hooksRunTool, "tool", "", "tool name the hooks are matched against")
	hooksRunCmd.Flags().StringVar(&hooksRunInput, "input", "{}", "tool input JSON handed to the hooks")
	hooksRunCmd.Flags().StringVar(&hooksRunResponse, "response", "", "tool output handed to PostToolUse hooks")
	hooksRunCmd.Flags().StringVar(&hooksRunPrompt, "prompt", "", "prompt handed to UserPromptSubmit hooks")
	hooksCmd.AddCommand(hooksRunCmd)
}

// hookEvents maps what a user may type to the canonical event name. It
// mirrors the config loader's normalization rather than importing it: that
// function is unexported, and duplicating three names is cheaper than
// widening the config package's API for a debugging command.
var hookEvents = map[string]string{
	"pretooluse":       hooks.EventPreToolUse,
	"posttooluse":      hooks.EventPostToolUse,
	"userpromptsubmit": hooks.EventUserPromptSubmit,
	"sessionstart":     hooks.EventSessionStart,
	"sessionend":       hooks.EventSessionEnd,
	"precompact":       hooks.EventPreCompact,
	"stop":             hooks.EventStop,
	"subagentstop":     hooks.EventSubagentStop,
}

func canonicalHookEvent(name string) (string, error) {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", ""))
	event, ok := hookEvents[key]
	if !ok {
		return "", fmt.Errorf(
			"unknown hook event %q: use PreToolUse, PostToolUse, UserPromptSubmit, SessionStart, SessionEnd, PreCompact, Stop, or SubagentStop",
			name)
	}
	return event, nil
}

func runHooksRun(cmd *cobra.Command, args []string) error {
	event, err := canonicalHookEvent(args[0])
	if err != nil {
		return err
	}

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")

	cfg, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}

	configured := cfg.Config().Hooks[event]
	if len(configured) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No %s hooks configured.\n", event)
		return nil
	}

	runner := hooks.NewRunner(configured, cfg.WorkingDir(), cfg.WorkingDir())

	// A session ID is required by the payload but means nothing here: no
	// session is running, and a hook that looks one up should see that
	// rather than a plausible-looking ID that belongs to someone else.
	const sessionID = "hooks-run"

	var result hooks.AggregateResult
	switch event {
	case hooks.EventUserPromptSubmit:
		result, err = runner.RunPrompt(cmd.Context(), sessionID, hooksRunPrompt)
	case hooks.EventPostToolUse:
		result, err = runner.RunPost(cmd.Context(), sessionID, hooksRunTool, hooksRunInput, hooksRunResponse)
	case hooks.EventSessionStart, hooks.EventSessionEnd, hooks.EventPreCompact:
		result, err = runner.RunSessionEvent(cmd.Context(), event, sessionID)
	case hooks.EventStop, hooks.EventSubagentStop:
		result, err = runner.RunStop(cmd.Context(), event, sessionID, false, "Finished.")
	default:
		result, err = runner.Run(cmd.Context(), event, sessionID, hooksRunTool, hooksRunInput)
	}
	if err != nil {
		return fmt.Errorf("running %s hooks: %w", event, err)
	}

	return printHookResult(cmd.OutOrStdout(), result)
}

func printHookResult(out io.Writer, result hooks.AggregateResult) error {
	if result.HookCount == 0 {
		_, err := fmt.Fprintln(out, "No hooks matched.")
		return err
	}

	for _, h := range result.Hooks {
		matcher := h.Matcher
		if matcher == "" {
			matcher = "(all tools)"
		}
		fmt.Fprintf(out, "%s [%s]: %s", h.Name, matcher, h.Decision)
		if h.Halt {
			fmt.Fprint(out, ", halt")
		}
		if h.InputRewrite {
			fmt.Fprint(out, ", rewrote input")
		}
		if h.Reason != "" {
			fmt.Fprintf(out, " -- %s", h.Reason)
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "\nresult: %s", result.Decision)
	if result.Halt {
		fmt.Fprint(out, ", halt")
	}
	fmt.Fprintln(out)
	if result.Reason != "" {
		fmt.Fprintf(out, "reason: %s\n", result.Reason)
	}
	if result.Context != "" {
		fmt.Fprintf(out, "context: %s\n", result.Context)
	}
	if result.UpdatedInput != "" {
		fmt.Fprintf(out, "updated input: %s\n", result.UpdatedInput)
	}
	return nil
}
