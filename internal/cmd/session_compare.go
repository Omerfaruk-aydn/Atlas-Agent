package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/event"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/replay"
	"github.com/spf13/cobra"
)

var sessionCompareJSON bool

var sessionCompareCmd = &cobra.Command{
	Use:   "compare <a> <b>",
	Short: "Compare two sessions or branches in depth",
	Long: "Compare two sessions, typically a session and a branch of it or two branches of the same " +
		"point: where their messages diverge, how tool use differs, which files each changed and whether " +
		"they ended with the same content, token use and cost, and how each concluded. " +
		"Use --json for machine-readable output. IDs can be a UUID, full hash, or hash prefix.",
	Args: cobra.ExactArgs(2),
	RunE: runSessionCompare,
}

func init() {
	sessionCompareCmd.Flags().BoolVar(&sessionCompareJSON, "json", false, "output in JSON format")
	sessionCmd.AddCommand(sessionCompareCmd)
}

func runSessionCompare(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := svc.messages.FlushAll(ctx); err != nil {
		return fmt.Errorf("failed to flush messages: %w", err)
	}
	var sides [2]replay.Side
	for i := range sides {
		sess, err := resolveSessionID(ctx, svc.sessions, args[i])
		if err != nil {
			return err
		}
		sides[i], err = loadReplaySide(ctx, svc, sess)
		if err != nil {
			return err
		}
	}
	return writeComparison(cmd.OutOrStdout(), replay.Compare(sides[0], sides[1]), sessionCompareJSON)
}

func loadReplaySide(ctx context.Context, svc *sessionServices, sess session.Session) (replay.Side, error) {
	msgs, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return replay.Side{}, fmt.Errorf("failed to list messages: %w", err)
	}
	versions, err := svc.history.ListBySession(ctx, sess.ID)
	if err != nil {
		return replay.Side{}, fmt.Errorf("failed to list file history: %w", err)
	}
	return replay.Side{
		ID:               session.HashID(sess.ID),
		Title:            sess.Title,
		Messages:         msgs,
		PromptTokens:     sess.PromptTokens,
		CompletionTokens: sess.CompletionTokens,
		Cost:             sess.Cost,
		Files:            changedFiles(versions),
	}, nil
}

// changedFiles reduces a session's file history to the files it changed and
// their final content. A version-0 row with no message is a baseline of a
// file the session merely read before changing; on its own it is no change.
func changedFiles(versions []history.File) map[string]string {
	latest := map[string]history.File{}
	changed := map[string]bool{}
	for _, v := range versions {
		if cur, ok := latest[v.Path]; !ok || v.Version >= cur.Version {
			latest[v.Path] = v
		}
		if v.Version > history.InitialVersion || v.MessageID != "" {
			changed[v.Path] = true
		}
	}
	files := make(map[string]string, len(changed))
	for p := range changed {
		files[p] = latest[p].Content
	}
	return files
}

func writeComparison(w io.Writer, c replay.Comparison, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(c)
	}
	p := &printer{w: w}
	p.f("A: %s  %s\n", short(c.A.ID), c.A.Title)
	p.f("B: %s  %s\n\n", short(c.B.ID), c.B.Title)

	p.f("Messages:    %d vs %d\n", c.A.Messages, c.B.Messages)
	p.f("Tool calls:  %d vs %d  (errors %d vs %d)\n", c.A.ToolCalls, c.B.ToolCalls, c.A.ToolErrors, c.B.ToolErrors)
	p.f("Files:       %d vs %d changed\n", c.A.FilesChanged, c.B.FilesChanged)
	p.f("Tokens:      %d in / %d out vs %d in / %d out\n",
		c.A.PromptTokens, c.A.CompletionTokens, c.B.PromptTokens, c.B.CompletionTokens)
	p.f("Cost:        $%.4f vs $%.4f  (%+.4f)\n", c.A.Cost, c.B.Cost, c.B.Cost-c.A.Cost)
	if len(c.A.Models) > 0 || len(c.B.Models) > 0 {
		p.f("Models:      %v vs %v\n", c.A.Models, c.B.Models)
	}

	p.f("\nDivergence: %d identical leading step(s)\n", c.Divergence.CommonSteps)
	switch {
	case c.Divergence.A == "" && c.Divergence.B == "":
		p.f("  the sessions are identical\n")
	default:
		p.f("  A next: %s\n", orEnds(c.Divergence.A))
		p.f("  B next: %s\n", orEnds(c.Divergence.B))
	}

	if len(c.Tools) > 0 {
		p.f("\nTool call differences:\n")
		for _, t := range c.Tools {
			p.f("  %s: %d -> %d\n", t.Name, t.A, t.B)
		}
	}

	p.f("\nFiles:\n")
	files := c.Files
	if len(files.OnlyA)+len(files.OnlyB)+len(files.Differ)+len(files.Same) == 0 {
		p.f("  neither session changed any file\n")
	}
	for _, f := range files.OnlyA {
		p.f("  - %s (only in A)\n", f)
	}
	for _, f := range files.OnlyB {
		p.f("  + %s (only in B)\n", f)
	}
	for _, f := range files.Differ {
		p.f("  ~ %s (different content)\n", f)
	}
	for _, f := range files.Same {
		p.f("    %s (same content)\n", f)
	}

	p.f("\nOutcome:\n")
	p.f("  A [%s]: %s\n", orNone(c.A.Finish), orNone(c.A.Outcome))
	p.f("  B [%s]: %s\n", orNone(c.B.Finish), orNone(c.B.Outcome))
	return p.err
}

// printer remembers the first write error so a long report reads straight
// through instead of checking every line.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

func short(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

func orEnds(s string) string {
	if s == "" {
		return "(session ends)"
	}
	return s
}
