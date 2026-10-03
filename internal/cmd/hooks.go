package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/spf13/cobra"
)

var (
	hooksListTool string
	hooksListJSON bool
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Inspect configured hooks",
}

var hooksListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List configured hooks",
	Long: "List the hooks this workspace has configured, by event, with the matcher and command of each. " +
		"Pass --tool to see only the hooks that would fire for that tool. Nothing is executed. " +
		"Use --json for machine-readable output.",
	Args: cobra.NoArgs,
	RunE: runHooksList,
}

func init() {
	hooksListCmd.Flags().StringVar(&hooksListTool, "tool", "", "show only hooks that would fire for this tool")
	hooksListCmd.Flags().BoolVar(&hooksListJSON, "json", false, "output in JSON format")
	hooksCmd.AddCommand(hooksListCmd)
	rootCmd.AddCommand(hooksCmd)
}

func runHooksList(cmd *cobra.Command, _ []string) error {
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
	return listHooks(cmd, cfg, hooksListTool)
}

// listHooks is split out from runHooksList for the same reason
// listProviders is split from runProviderList: a test can build a
// *config.ConfigStore directly but cannot inject one into a function that
// calls config.Init itself.
//
// It builds a hooks.Runner per event rather than reading the config
// directly, so what it prints for --tool is decided by the same matcher
// code that decides it at run time. A hook whose matcher does not compile
// is dropped by the runner and so is absent here too -- which is the honest
// answer, since it will never fire either.
// jsonHook is one hook's wire form for --json.
type jsonHook struct {
	Prompt   string `json:"prompt,omitempty"`
	Agent    string `json:"agent,omitempty"`
	MaxFires int    `json:"max_fires,omitempty"`
	Event    string `json:"event"`
	Name     string `json:"name"`
	Matcher  string `json:"matcher,omitempty"`
	Command  string `json:"command"`
}

func listHooks(cmd *cobra.Command, cfg *config.ConfigStore, toolName string) error {
	configured := cfg.Config().Hooks

	var events []string
	for event := range configured {
		events = append(events, event)
	}
	sort.Strings(events)

	out := cmd.OutOrStdout()

	if hooksListJSON {
		var jsonHooks []jsonHook
		for _, event := range events {
			runner := hooks.NewRunner(configured[event], cfg.WorkingDir(), cfg.WorkingDir())
			list := runner.Hooks()
			if toolName != "" {
				list = runner.MatchingHooks(toolName)
			}
			for _, h := range list {
				jsonHooks = append(jsonHooks, jsonHook{
					Prompt: h.Prompt, Agent: h.Agent, MaxFires: h.MaxFires,
					Event:   event,
					Name:    h.DisplayName(),
					Matcher: h.Matcher,
					Command: h.Command,
				})
			}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(jsonHooks)
	}

	var shown int
	for _, event := range events {
		runner := hooks.NewRunner(configured[event], cfg.WorkingDir(), cfg.WorkingDir())

		list := runner.Hooks()
		if toolName != "" {
			list = runner.MatchingHooks(toolName)
		}
		if len(list) == 0 {
			continue
		}

		fmt.Fprintf(out, "%s\n", event)
		for _, h := range list {
			matcher := h.Matcher
			if matcher == "" {
				matcher = "(all tools)"
			}
			action := h.Command
			if h.Prompt != "" {
				action = "agent " + h.Agent + ": " + h.Prompt
			}
			fmt.Fprintf(out, "  %s [%s]\n    %s\n", h.DisplayName(), matcher, action)
			shown++
		}
	}

	if shown == 0 {
		if toolName != "" {
			fmt.Fprintf(out, "No hooks would fire for %s.\n", toolName)
		} else {
			fmt.Fprintln(out, "No hooks configured.")
		}
	}
	return nil
}
