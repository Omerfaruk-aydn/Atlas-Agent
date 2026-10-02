package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-term"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/spf13/cobra"
)

func newFindingsCommand() *cobra.Command {
	command := &cobra.Command{Use: "findings", Short: "Inspect review findings and record direct user waivers without an LLM"}
	command.AddCommand(&cobra.Command{Use: "list <session-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		findings, err := checkpointStore(cmd, root).TaskFindings(cmd.Context(), args[0], "")
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(findings)
	}})
	waive := &cobra.Command{Use: "waive <session-id> <finding-id>", Args: cobra.ExactArgs(2), Short: "Waive one finding with a reason and interactive confirmation; never marks it verified", RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("AI_AGENT") != "" || os.Getenv("ATLAS-AGENT") != "" || os.Getenv("AGENT") == "Atlas-Agent" || os.Getenv("ATLAS_TOOL_CALL_ID") != "" {
			return fmt.Errorf("agent execution cannot authorize a finding waiver; use a direct user terminal")
		}
		input, ok := cmd.InOrStdin().(*os.File)
		if !ok || !term.IsTerminal(input.Fd()) {
			return fmt.Errorf("finding waivers require direct interactive terminal confirmation; piped or agent tool input is not accepted")
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		ref, err := checkpointStore(cmd, root).WaiveFinding(cmd.Context(), args[0], args[1], reason, func(ctx context.Context, finding engineering.Finding, reason string) (bool, error) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			challenge := "waive " + finding.ID[:12]
			fmt.Fprintf(cmd.ErrOrStderr(), "Finding %s (severity %d), %s:%d-%d\n%s\nExpected: %s\nReason: %s\nType %q to accept this finding for the current source: ", finding.ID, finding.Severity, finding.Path, finding.StartLine, finding.EndLine, finding.Issue, finding.Expected, reason, challenge)
			text, err := bufio.NewReader(input).ReadString('\n')
			if err != nil {
				return false, err
			}
			return strings.TrimSpace(text) == challenge, nil
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"status": "waived", "verified": false, "reference": ref})
	}}
	waive.Flags().String("reason", "", "Explicit reason for accepting the finding")
	command.AddCommand(waive)
	return command
}
