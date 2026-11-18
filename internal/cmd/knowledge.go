package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/knowledge"
	"github.com/spf13/cobra"
)

func newKnowledgeCommand() *cobra.Command {
	store := func(cmd *cobra.Command) (*knowledge.Store, error) {
		root, err := workflowRoot(cmd)
		if err != nil {
			return nil, err
		}
		return knowledge.New(knowledge.DirFor(root), root), nil
	}
	c := &cobra.Command{
		Use:   "knowledge",
		Short: "Review the project notes the agent proposed",
		Long: "The agent can propose short notes about the project (see the knowledge option). " +
			"Nothing is used until you approve it here. Approved notes appear as a one-line index in every session, " +
			"marked stale when the files they came from change.",
	}
	c.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			items, err := s.List()
			if err != nil {
				return err
			}
			type row struct {
				ID      string   `json:"id"`
				Title   string   `json:"title"`
				Summary string   `json:"summary"`
				Status  string   `json:"status"`
				Stale   []string `json:"stale,omitempty"`
			}
			rows := make([]row, 0, len(items))
			for _, it := range items {
				rows = append(rows, row{it.ID, it.Title, it.Summary, string(it.Status), s.Stale(it)})
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
		}},
		&cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			it, err := s.Get(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "# %s [%s]\n%s\n", it.Title, it.Status, it.Body)
			return nil
		}},
		&cobra.Command{Use: "approve <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			return s.Approve(args[0])
		}},
		&cobra.Command{Use: "remove <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			return s.Remove(args[0])
		}},
	)
	return c
}

func init() { rootCmd.AddCommand(newKnowledgeCommand()) }
