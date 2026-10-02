package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/spf13/cobra"
)

var agentListJSON bool

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Inspect and manage subagents",
	Long: "A subagent is a named task definition (a Markdown file with a description and, optionally, " +
		"a model role) that work can be handed to instead of running on the session's primary model -- " +
		"e.g. a \"research\" subagent pointed at a model role good at digging through documentation, or a " +
		"\"frontend\" subagent pointed at a model role tuned for UI work. See `atlas models roles`.",
}

var agentListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List discovered subagents",
	Long:    "List every subagent this workspace discovers, with its model role and whether that role resolves.",
	Args:    cobra.NoArgs,
	RunE:    runAgentList,
}

var agentShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a subagent's full definition",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentShow,
}

func init() {
	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output in JSON format")
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentShowCmd)
	rootCmd.AddCommand(agentCmd)
}

// discoverConfiguredSubagents reads cfg.Options.SubagentsPaths and returns
// every subagent discovered there, deduplicated by name (a later directory
// in the list -- project-level, typically -- overrides an earlier one).
func discoverConfiguredSubagents(cfg *config.ConfigStore) []*subagents.Subagent {
	var paths []string
	if opts := cfg.Config().Options; opts != nil {
		paths = opts.SubagentsPaths
	}
	return subagents.Discover(paths)
}

// jsonSubagent is one subagent's wire form for --json.
type jsonSubagent struct {
	MeasuredSelection bool                    `json:"measured_selection,omitempty"`
	SelectionError    string                  `json:"selection_error,omitempty"`
	ResolvedModel     string                  `json:"resolved_model,omitempty"`
	Contract          *subagents.RoleContract `json:"contract,omitempty"`
	ReadOnly          bool                    `json:"read_only,omitempty"`
	AllowCommands     bool                    `json:"allow_commands,omitempty"`
	Name              string                  `json:"name"`
	Description       string                  `json:"description"`
	Model             string                  `json:"model,omitempty"`
	ModelResolves     bool                    `json:"model_resolves"`
}

func runAgentList(cmd *cobra.Command, _ []string) error {
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
	return listSubagents(cmd, cfg)
}

// listSubagents is split out from runAgentList for the same reason
// listProviders is split from runProviderList: a test can build a
// *config.ConfigStore directly but cannot inject one into a function that
// calls config.Init itself.
func listSubagents(cmd *cobra.Command, cfg *config.ConfigStore) error {
	all := discoverConfiguredSubagents(cfg)
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	out := cmd.OutOrStdout()

	if agentListJSON {
		listed := make([]jsonSubagent, 0, len(all))
		for _, s := range all {
			_, resolves := cfg.Config().ResolveRole(s.Model)
			measured, enabled, selectionErr := cfg.ResolveMeasuredRole(s.Model)
			var selectionError, resolvedModel string
			if enabled {
				resolves = selectionErr == nil
				if selectionErr != nil {
					selectionError = selectionErr.Error()
				} else {
					resolvedModel = measured.Provider + "/" + measured.Model
				}
			}
			listed = append(listed, jsonSubagent{
				MeasuredSelection: enabled, SelectionError: selectionError, ResolvedModel: resolvedModel,
				Contract: s.Contract, ReadOnly: s.ReadOnly, AllowCommands: s.AllowCommands,
				Name:          s.Name,
				Description:   s.Description,
				Model:         s.Model,
				ModelResolves: s.Model == "" || resolves,
			})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(listed)
	}

	if len(all) == 0 {
		fmt.Fprintln(out, "No subagents found.")
		return nil
	}

	for _, s := range all {
		model := "(session's primary model)"
		if s.Model != "" {
			model = s.Model
			measured, enabled, selectionErr := cfg.ResolveMeasuredRole(s.Model)
			if enabled {
				if selectionErr != nil {
					model += " (measurement unavailable: " + selectionErr.Error() + ")"
				} else {
					model += " -> " + measured.Provider + "/" + measured.Model + " (measured)"
				}
			} else if _, ok := cfg.Config().ResolveRole(s.Model); !ok {
				model += " (unresolved)"
			}
		}
		fmt.Fprintf(out, "%s -- %s\n  %s\n", s.Name, model, s.Description)
	}
	return nil
}

func runAgentShow(cmd *cobra.Command, args []string) error {
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

	all := discoverConfiguredSubagents(cfg)
	s, ok := subagents.Find(all, args[0])
	if !ok {
		return fmt.Errorf("no subagent named %q found", args[0])
	}

	rendered, err := subagents.Render(s)
	if err != nil {
		return fmt.Errorf("rendering subagent: %w", err)
	}
	_, err = cmd.OutOrStdout().Write(rendered)
	return err
}
