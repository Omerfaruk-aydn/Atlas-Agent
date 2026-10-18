package cmd

import (
	"fmt"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/spf13/cobra"
)

var statsHarnessCmd = &cobra.Command{
	Use:   "harness",
	Short: "Show how the agent harness behaved in recent sessions",
	Long: `Summarize the harness events recorded in this project: how often the
verification contract sent work back, which tool searches found nothing,
whether shell nudges were followed, how often turns were stopped for looping
and how often context was compacted. Use it to pick the next improvement
from real sessions rather than guesses.`,
	Example: `  atlas stats harness
  atlas stats harness --days 30`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dataDir, _ := cmd.Flags().GetString("data-dir")
		days, _ := cmd.Flags().GetInt("days")
		if days <= 0 {
			return fmt.Errorf("--days must be positive")
		}
		if dataDir == "" {
			cfg, err := config.Init("", dataDir, false)
			if err != nil {
				return fmt.Errorf("failed to initialize config: %w", err)
			}
			dataDir = cfg.Config().Options.DataDirectory
		}
		since := time.Now().AddDate(0, 0, -days)
		events, err := harnessmetrics.Load(dataDir, since)
		if err != nil {
			return fmt.Errorf("failed to read harness events: %w", err)
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), harnessmetrics.Format(harnessmetrics.Summarize(events), since))
		return err
	},
}

func init() {
	statsHarnessCmd.Flags().Int("days", 14, "How many days back to summarize")
	statsCmd.AddCommand(statsHarnessCmd)
}
