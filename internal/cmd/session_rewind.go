package cmd

import (
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/spf13/cobra"
)

var sessionRewindApply bool

var sessionRewindCmd = &cobra.Command{
	Use:   "rewind <id> <message-id>",
	Short: "Fork a session at an earlier message and restore files to before it",
	Long: "Fork a session at a chosen user message: the new session gets a copy of the source " +
		"session's messages up to and including that one, and the working directory's files are " +
		"restored to their content before it. Nothing is run. The source session is never modified " +
		"or deleted. " +
		"Restoring files is hard to reverse, so this only previews what would change (files written and " +
		"deleted) unless --apply is given. ID can be a UUID, full hash, or hash prefix.",
	Args: cobra.ExactArgs(2),
	RunE: runSessionRewind,
}

func init() {
	sessionRewindCmd.Flags().BoolVar(&sessionRewindApply, "apply", false,
		"actually fork the session and restore files, instead of only previewing")
	sessionCmd.AddCommand(sessionRewindCmd)
}

func runSessionRewind(cmd *cobra.Command, args []string) error {
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
	messageID := args[1]

	svcRewind := rewind.NewService(svc.sessions, svc.messages, svc.history)

	if !sessionRewindApply {
		written, deleted, err := svcRewind.Preview(ctx, sess.ID, messageID)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Would write %d file(s) and delete %d file(s). Re-run with --apply to do it.\n", written, deleted)
		return nil
	}

	result, err := svcRewind.ForkAt(ctx, sess.ID, messageID)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Forked into session %s: wrote %d file(s), deleted %d file(s).\n",
		session.HashID(result.Session.ID), result.FilesWritten, result.FilesDeleted)
	return nil
}
