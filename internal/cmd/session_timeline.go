package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/replay"
	"github.com/spf13/cobra"
)

var sessionTimelineJSON bool

var sessionTimelineCmd = &cobra.Command{
	Use:   "timeline <id>",
	Short: "Show a session as a numbered step timeline",
	Long: "Show every message of a session as a numbered step: who spoke, which model answered, " +
		"which tools were called and how many came back as errors. Steps marked with * cannot be used " +
		"as a branch point because a tool call in them is still unanswered; use the step numbers with " +
		"'session branch --at'. Use --json for machine-readable output. " +
		"ID can be a UUID, full hash, or hash prefix.",
	Args: cobra.ExactArgs(1),
	RunE: runSessionTimeline,
}

func init() {
	sessionTimelineCmd.Flags().BoolVar(&sessionTimelineJSON, "json", false, "output in JSON format")
	sessionCmd.AddCommand(sessionTimelineCmd)
}

func runSessionTimeline(cmd *cobra.Command, args []string) error {
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
	if err := svc.messages.FlushAll(ctx); err != nil {
		return fmt.Errorf("failed to flush messages: %w", err)
	}
	msgs, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}
	return writeTimeline(cmd.OutOrStdout(), sess, replay.BuildTimeline(msgs), sessionTimelineJSON)
}

type timelineOutput struct {
	ID    string        `json:"id"`
	Title string        `json:"title"`
	Steps []replay.Step `json:"steps"`
}

func writeTimeline(w io.Writer, sess session.Session, steps []replay.Step, asJSON bool) error {
	if asJSON {
		if steps == nil {
			steps = []replay.Step{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(timelineOutput{ID: session.HashID(sess.ID), Title: sess.Title, Steps: steps})
	}

	title := strings.ReplaceAll(sess.Title, "\n", " ")
	if _, err := fmt.Fprintf(w, "Session %s  %s  (%d step(s))\n", session.HashID(sess.ID)[:7], title, len(steps)); err != nil {
		return err
	}
	if len(steps) == 0 {
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "STEP\tTIME\tROLE\tMODEL\tSUMMARY")
	unbranchable := false
	for _, s := range steps {
		num := fmt.Sprintf("%d", s.Number)
		if !s.Branchable {
			num += "*"
			unbranchable = true
		}
		summary := s.Summary
		if s.Errors > 0 {
			summary += fmt.Sprintf("  [%d tool error(s)]", s.Errors)
		}
		if s.Finish != "" && s.Finish != "end_turn" && s.Finish != "tool_use" {
			summary += "  [" + s.Finish + "]"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", num, formatStepTime(s.CreatedAt), s.Role, s.Model, summary)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if unbranchable {
		_, err := fmt.Fprintln(w, "\n* a tool call here is still unanswered, so a branch cannot end at this step.")
		return err
	}
	return nil
}

func formatStepTime(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("15:04:05")
}
