package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/spf13/cobra"
)

var (
	sessionBranchAt           string
	sessionBranchModel        string
	sessionBranchPrompt       string
	sessionBranchRestoreFiles bool
	sessionBranchDryRun       bool
	sessionBranchRun          bool
)

var sessionBranchCmd = &cobra.Command{
	Use:   "branch <id> --at <step>",
	Short: "Fork a new session from any step of an existing one",
	Long: "Create a new session holding a copy of the source session's messages up to and including a step " +
		"(see 'session timeline' for step numbers, or pass a message ID). The source session is never " +
		"modified. Unlike 'session rewind' the step can be any message whose tool calls are all answered, " +
		"and files on disk are left alone unless --restore-files is given.\n\n" +
		"--model and --prompt shape how the branch continues. --prompt is the message to send next; if the " +
		"step is a user message it is replaced by the prompt, which is how to retry a turn with different " +
		"wording. Nothing is sent unless --run is given; otherwise the exact continue command is printed. " +
		"Use --dry-run to preview without creating anything.",
	Args: cobra.ExactArgs(1),
	RunE: runSessionBranch,
}

func init() {
	f := sessionBranchCmd.Flags()
	f.StringVar(&sessionBranchAt, "at", "", "step number or message ID the branch ends at (required)")
	f.StringVar(&sessionBranchModel, "model", "", "model to continue the branch with (provider/model accepted)")
	f.StringVar(&sessionBranchPrompt, "prompt", "", "message to send next on the branch")
	f.BoolVar(&sessionBranchRestoreFiles, "restore-files", false, "also restore files on disk to their state at that step")
	f.BoolVar(&sessionBranchDryRun, "dry-run", false, "show what would happen without creating a session or touching files")
	f.BoolVar(&sessionBranchRun, "run", false, "send --prompt on the new branch right away (needs --prompt)")
	_ = sessionBranchCmd.MarkFlagRequired("at")
	sessionCmd.AddCommand(sessionBranchCmd)
}

func runSessionBranch(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	if sessionBranchRun && sessionBranchPrompt == "" {
		return fmt.Errorf("--run needs --prompt")
	}
	if sessionBranchRun && sessionBranchDryRun {
		return fmt.Errorf("--run and --dry-run cannot be combined")
	}

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	sess, err := resolveSessionID(ctx, svc.sessions, args[0])
	if err != nil {
		return err
	}

	brancher := rewind.NewBrancher(svc.sessions, svc.messages, svc.history)
	res, err := brancher.Branch(ctx, rewind.BranchOptions{
		SourceID:        sess.ID,
		MessageID:       sessionBranchAt,
		ReplaceLastUser: sessionBranchPrompt != "",
		RestoreFiles:    sessionBranchRestoreFiles,
		DryRun:          sessionBranchDryRun,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if sessionBranchDryRun {
		fmt.Fprintf(out, "Would create a branch of %d message(s)", res.Messages)
		if res.Dropped {
			fmt.Fprint(out, " (the step's user message replaced by --prompt)")
		}
		if sessionBranchRestoreFiles {
			fmt.Fprintf(out, ", write %d file(s) and delete %d file(s)", res.FilesWritten, res.FilesDeleted)
		}
		fmt.Fprintln(out, ". Re-run without --dry-run to do it.")
		return nil
	}

	id := session.HashID(res.Session.ID)
	fmt.Fprintf(out, "Branched into session %s with %d message(s)", id, res.Messages)
	if sessionBranchRestoreFiles {
		fmt.Fprintf(out, "; wrote %d file(s), deleted %d file(s)", res.FilesWritten, res.FilesDeleted)
	}
	fmt.Fprintln(out, ".")

	cont := continueArgs(id, sessionBranchModel, sessionBranchPrompt)
	if sessionBranchRun {
		return runBranchContinuation(ctx, cmd, cont)
	}
	fmt.Fprintf(out, "Continue with: atlas-agent %s\n", shellJoin(cont))
	return nil
}

// continueArgs builds the arguments of the command that continues the
// branch: a headless run when there is a prompt, the TUI otherwise.
func continueArgs(sessionID, model, prompt string) []string {
	if prompt == "" {
		args := []string{"-s", sessionID}
		if model != "" {
			args = append(args, "-m", model)
		}
		return args
	}
	args := []string{"run", "-s", sessionID}
	if model != "" {
		args = append(args, "-m", model)
	}
	return append(args, prompt)
}

func shellJoin(args []string) string {
	var sb []byte
	for i, a := range args {
		if i > 0 {
			sb = append(sb, ' ')
		}
		if a != "" && !needsQuoting(a) {
			sb = append(sb, a...)
			continue
		}
		sb = strconv.AppendQuote(sb, a)
	}
	return string(sb)
}

func needsQuoting(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '/' || r == '.' || r == ':':
		default:
			return true
		}
	}
	return false
}

// runBranchContinuation re-executes this binary on the new branch so the
// prompt goes through the same non-interactive path as 'atlas-agent run'.
func runBranchContinuation(ctx context.Context, cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate the atlas-agent executable: %w", err)
	}
	if dataDir, _ := cmd.Flags().GetString("data-dir"); dataDir != "" {
		args = append([]string{"--data-dir", dataDir}, args...)
	}
	c := exec.CommandContext(ctx, exe, args...)
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}
