package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	contextCmd := &cobra.Command{Use: "context", Short: "Inspect actual model request metadata and manage optional session context"}
	contextCmd.AddCommand(&cobra.Command{Use: "inspect SESSION", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		manifest, prefs, err := store.ReadContext(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			Manifest    any `json:"manifest"`
			Preferences any `json:"preferences"`
		}{manifest, prefs})
	}})
	for _, action := range []string{"pin", "unpin", "exclude", "include"} {
		contextCmd.AddCommand(&cobra.Command{Use: action + " SESSION VALUE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			store, err := workflowStore(cmd)
			if err != nil {
				return err
			}
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			return store.ContextControl(cmd.Context(), root, args[0], "context_"+action, args[1])
		}})
	}
	profiles := &cobra.Command{Use: "profiles", Short: "Inspect and select usage profiles combining model, tools, limits and output behavior"}
	load := func(cmd *cobra.Command) (*config.ConfigStore, error) {
		root, err := workflowRoot(cmd)
		if err != nil {
			return nil, err
		}
		dataDir, _ := cmd.Flags().GetString("data-dir")
		return config.Load(root, dataDir, false)
	}
	profiles.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := load(cmd)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(cfg.Config().UsageProfiles())
	}})
	profiles.AddCommand(&cobra.Command{Use: "use NAME", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := load(cmd)
		if err != nil {
			return err
		}
		if err = cfg.OverrideUsageProfile(args[0]); err != nil {
			return err
		}
		if err = cfg.SetConfigField(config.ScopeWorkspace, "options.usage_profile", args[0]); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Usage profile selected:", args[0])
		return nil
	}})
	batch := &cobra.Command{Use: "batches SESSION", Args: cobra.ExactArgs(1), Short: "Inspect durable batch rows without invoking a model", RunE: func(cmd *cobra.Command, args []string) error {
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		reports, err := store.AgentBatches(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(reports)
	}}
	rootCmd.AddCommand(contextCmd, profiles, batch)
}
