package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/evaluation"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/spf13/cobra"
)

func newEvalCorpusCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "corpus",
		Short: "List and export requests that ended with unmet checks or findings",
		Long: `Atlas saves the requests that ended badly in real use: the verification
contract still unmet after its round limit, or evaluator findings left
unresolved. Export one as a bench task to measure a harness change against
the work it actually struggles with.`,
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List saved cases, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dataDir, err := corpusDataDir(cmd)
			if err != nil {
				return err
			}
			cases, err := harnessmetrics.LoadCases(dataDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cases) == 0 {
				_, err := fmt.Fprintln(out, "No failure cases saved yet.")
				return err
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tWHEN\tREASON\tCHECKS\tREQUEST")
			for i := len(cases) - 1; i >= 0; i-- {
				c := cases[i]
				request := strings.Join(strings.Fields(c.Request), " ")
				if len(request) > 60 {
					request = request[:60] + "…"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", c.ID, time.UnixMilli(c.Time).Format("2006-01-02 15:04"), c.Reason, len(c.Checks), request)
			}
			return w.Flush()
		},
	})
	var suite string
	var check []string
	export := &cobra.Command{
		Use:   "export <case-id>...",
		Short: "Turn saved cases into bench tasks",
		Example: `  atlas eval corpus export 20261016-141500-abcd1234 --suite bench/corpus
  atlas eval corpus export 20261016-141500-abcd1234 --suite bench/corpus --check go,test,./...`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if suite == "" {
				return fmt.Errorf("--suite is required")
			}
			dataDir, err := corpusDataDir(cmd)
			if err != nil {
				return err
			}
			for _, id := range args {
				c, err := harnessmetrics.LoadCase(dataDir, id)
				if err != nil {
					return err
				}
				dir, err := evaluation.ExportCase(cmd.Context(), c, suite, check)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Exported %s to %s\n", id, dir); err != nil {
					return err
				}
			}
			return nil
		},
	}
	export.Flags().StringVar(&suite, "suite", "", "Suite directory to write the tasks into")
	export.Flags().StringSliceVar(&check, "check", nil, "Judge command as comma-separated argv, replacing the case's recorded checks")
	command.AddCommand(export)
	return command
}

func corpusDataDir(cmd *cobra.Command) (string, error) {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	if dataDir != "" {
		return dataDir, nil
	}
	cfg, err := config.Init("", dataDir, false)
	if err != nil {
		return "", fmt.Errorf("failed to initialize config: %w", err)
	}
	return cfg.Config().Options.DataDirectory, nil
}
