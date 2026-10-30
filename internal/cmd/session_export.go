package cmd

import (
	"fmt"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/export"
	"github.com/spf13/cobra"
)

var (
	sessionExportOutput  string
	sessionExportFormat  string
	sessionExportPreview bool
)

var sessionExportCmd = &cobra.Command{
	Use:   "export <id>",
	Short: "Export a session as Markdown or JSON",
	Long: "Export a session's conversation for saving or sharing outside the TUI. " +
		"Writes Markdown by default; use --format json for a machine-readable document instead. " +
		"Writes to stdout by default; use --output to write to a file instead. " +
		"ID can be a UUID, full hash, or hash prefix.",
	Args: cobra.ExactArgs(1),
	RunE: runSessionExport,
}

func init() {
	sessionExportCmd.Flags().StringVarP(&sessionExportOutput, "output", "o", "", "file to write to (default: stdout)")
	sessionExportCmd.Flags().StringVar(&sessionExportFormat, "format", "markdown", "output format: markdown, html or json")
	sessionExportCmd.Flags().BoolVar(&sessionExportPreview, "preview", false, "Preview the redacted export without writing a file")
	sessionCmd.AddCommand(sessionExportCmd)
}

func runSessionExport(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	sess, err := resolveSessionID(ctx, svc.sessions, args[0])
	if err != nil {
		return err
	}

	msgs, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	var doc string
	switch sessionExportFormat {
	case "markdown", "":
		doc = export.Markdown(sess, msgs)
	case "html":
		doc = export.HTML(sess, msgs)
	case "json":
		raw, err := export.JSON(sess, msgs)
		if err != nil {
			return fmt.Errorf("failed to build JSON export: %w", err)
		}
		doc = string(raw)
	default:
		return fmt.Errorf("unknown format %q: use markdown, html or json", sessionExportFormat)
	}

	if sessionExportOutput == "" || sessionExportPreview {
		_, err := fmt.Fprint(cmd.OutOrStdout(), doc)
		return err
	}

	if err := os.WriteFile(sessionExportOutput, []byte(doc), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", sessionExportOutput, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported to %s\n", sessionExportOutput)
	return nil
}
