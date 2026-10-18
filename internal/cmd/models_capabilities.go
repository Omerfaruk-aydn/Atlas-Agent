package cmd

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/spf13/cobra"
)

var modelsCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities [provider/model]",
	Short: "Show what Atlas sends for the selected models",
	Long: `Show the capabilities Atlas applies for a model on its provider path:
reasoning levels and the effort actually sent, which reasoning state is
replayed across turns, image input, prompt caching, token limits and every
configured setting that cannot be applied as requested.

Without arguments the configured large and small models are shown.`,
	Example: `atlas models capabilities
atlas models capabilities claude/claude-haiku-5-5 --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}
		dataDir, _ := cmd.Flags().GetString("data-dir")
		debug, _ := cmd.Flags().GetBool("debug")
		asJSON, _ := cmd.Flags().GetBool("json")
		store, err := config.Init(cwd, dataDir, debug)
		if err != nil {
			return err
		}
		cfg := store.Config()

		var selected []config.SelectedModel
		if len(args) == 1 {
			provider, model, ok := strings.Cut(args[0], "/")
			if !ok || provider == "" || model == "" {
				return fmt.Errorf("expected provider/model, got %q", args[0])
			}
			selected = append(selected, config.SelectedModel{Provider: provider, Model: model})
			for _, configured := range cfg.Models {
				if configured.Provider == provider && configured.Model == model {
					selected[0] = configured
				}
			}
		} else {
			for _, kind := range []config.SelectedModelType{config.SelectedModelTypeLarge, config.SelectedModelTypeSmall} {
				if m, ok := cfg.Models[kind]; ok && m.Model != "" {
					selected = append(selected, m)
				}
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("no model is configured")
		}

		var reports []agent.ProviderCapabilities
		for _, sel := range selected {
			providerCfg, ok := cfg.Providers.Get(sel.Provider)
			if !ok {
				return fmt.Errorf("provider %q is not configured", sel.Provider)
			}
			catalog := cfg.GetModel(sel.Provider, sel.Model)
			if catalog == nil {
				return fmt.Errorf("model %q is not in the catalog of provider %q", sel.Model, sel.Provider)
			}
			reports = append(reports, agent.ResolveCapabilities(agent.Model{CatwalkCfg: *catalog, ModelCfg: sel}, providerCfg))
		}
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(reports)
		}
		out := cmd.OutOrStdout()
		for i, r := range reports {
			if i > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "%s/%s (adapter %s)\n", r.Provider, r.Model, r.Adapter)
			levels := "none"
			if len(r.ReasoningLevels) > 0 {
				levels = strings.Join(slices.Clone(r.ReasoningLevels), ", ")
			}
			fmt.Fprintf(out, "  reasoning:        %t (levels: %s)\n", r.CanReason, levels)
			fmt.Fprintf(out, "  effort sent:      %s\n", orNone(r.EffectiveEffort))
			fmt.Fprintf(out, "  reasoning replay: %s\n", r.ReasoningReplay)
			fmt.Fprintf(out, "  images:           %t\n", r.Images)
			fmt.Fprintf(out, "  prompt cache:     %s\n", r.PromptCache)
			fmt.Fprintf(out, "  parallel tools:   %s\n", r.ParallelTools)
			fmt.Fprintf(out, "  context window:   %d, max output: %d\n", r.ContextWindow, r.MaxOutputTokens)
			if len(r.Adjustments) == 0 {
				fmt.Fprintln(out, "  adjustments:      none")
				continue
			}
			fmt.Fprintln(out, "  not applied as configured:")
			for _, adj := range r.Adjustments {
				fmt.Fprintf(out, "    - %s\n", adj)
			}
		}
		return nil
	},
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func init() {
	modelsCapabilitiesCmd.Flags().Bool("json", false, "Print the report as JSON")
	modelsCmd.AddCommand(modelsCapabilitiesCmd)
}
