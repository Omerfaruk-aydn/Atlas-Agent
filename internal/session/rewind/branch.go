package rewind

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/replay"
)

// BranchTagPrefix starts the tag that records which session a branch was
// forked from; the rest is the source session's hash ID.
const BranchTagPrefix = "branch-of:"

// BranchOptions says where and how to branch.
type BranchOptions struct {
	SourceID string
	// MessageID is the last message the branch keeps.
	MessageID string
	// ReplaceLastUser drops the kept range's final message when it is a
	// user message, so the caller can send a different one in its place.
	ReplaceLastUser bool
	// RestoreFiles rewrites the working directory's files to their state as
	// of the branch point. Without it the branch only copies the
	// conversation and leaves the disk alone.
	RestoreFiles bool
	// DryRun computes the result without creating a session or touching
	// any file.
	DryRun bool
}

// BranchResult describes what Branch did, or with DryRun would do.
type BranchResult struct {
	// Session is the new session; the zero value for a dry run.
	Session session.Session
	// Messages is how many messages the branch holds.
	Messages int
	// Dropped is the user message removed by ReplaceLastUser, if any.
	Dropped      bool
	FilesWritten int
	FilesDeleted int
}

// Brancher forks a session at any branchable step.
type Brancher struct {
	svc *service
}

// NewBrancher returns a Brancher over the given stores.
func NewBrancher(sessions session.Service, messages message.Service, files history.Service) *Brancher {
	return &Brancher{svc: &service{sessions: sessions, messages: messages, files: files}}
}

// Branch copies the source session's messages up to MessageID into a new
// top-level session tagged with its origin. Unlike ForkAt it accepts any
// step whose tool calls are all answered, not only user messages, and it
// leaves files alone unless asked. The source session is never modified.
func (b *Brancher) Branch(ctx context.Context, opts BranchOptions) (BranchResult, error) {
	s := b.svc
	source, err := s.sessions.Get(ctx, opts.SourceID)
	if err != nil {
		return BranchResult{}, fmt.Errorf("branch: loading source session: %w", err)
	}
	if err := s.messages.FlushAll(ctx); err != nil {
		return BranchResult{}, fmt.Errorf("branch: flushing pending message state: %w", err)
	}
	all, err := s.messages.List(ctx, source.ID)
	if err != nil {
		return BranchResult{}, fmt.Errorf("branch: listing messages: %w", err)
	}
	idx, err := replay.ResolveStep(all, opts.MessageID)
	if err != nil {
		return BranchResult{}, fmt.Errorf("branch: %w", err)
	}
	if err := replay.CheckBranchable(all, idx); err != nil {
		return BranchResult{}, fmt.Errorf("branch: %w", err)
	}
	kept := all[:idx+1]
	result := BranchResult{}
	if opts.ReplaceLastUser && kept[len(kept)-1].Role == message.User {
		kept = kept[:len(kept)-1]
		result.Dropped = true
	}
	result.Messages = len(kept)

	var resolved []history.ResolvedFile
	if opts.RestoreFiles {
		ids := make([]string, len(kept))
		for i, m := range kept {
			ids[i] = m.ID
		}
		resolved, err = s.files.ResolveAsOf(ctx, source.ID, ids)
		if err != nil {
			return BranchResult{}, fmt.Errorf("branch: resolving file state: %w", err)
		}
		for _, rf := range resolved {
			switch change(rf) {
			case changeWrite:
				result.FilesWritten++
			case changeDelete:
				result.FilesDeleted++
			}
		}
	}
	if opts.DryRun {
		return result, nil
	}

	fork, err := s.sessions.Create(ctx, branchTitle(source.Title))
	if err != nil {
		return BranchResult{}, fmt.Errorf("branch: creating session: %w", err)
	}
	fork, err = s.copyHistory(ctx, source, fork, kept)
	if err != nil {
		_ = s.sessions.Delete(context.WithoutCancel(ctx), fork.ID)
		return BranchResult{}, err
	}
	tags := append(append([]string{}, source.Tags...), BranchTagPrefix+session.HashID(source.ID))
	if err := s.sessions.SetTags(ctx, fork.ID, tags); err != nil {
		_ = s.sessions.Delete(context.WithoutCancel(ctx), fork.ID)
		return BranchResult{}, fmt.Errorf("branch: tagging session: %w", err)
	}
	fork.Tags = tags
	result.Session = fork

	// Files change only after the session exists and is complete, so a
	// failure above never leaves the disk half rewritten.
	result.FilesWritten, result.FilesDeleted = 0, 0
	for _, rf := range resolved {
		switch change(rf) {
		case changeDelete:
			if err := os.Remove(rf.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return result, fmt.Errorf("branch: removing %q: %w", rf.Path, err)
			}
			result.FilesDeleted++
		case changeWrite:
			if err := os.MkdirAll(filepath.Dir(rf.Path), 0o755); err != nil {
				return result, fmt.Errorf("branch: creating parent dir for %q: %w", rf.Path, err)
			}
			if err := os.WriteFile(rf.Path, []byte(*rf.Content), 0o644); err != nil {
				return result, fmt.Errorf("branch: writing %q: %w", rf.Path, err)
			}
			result.FilesWritten++
		}
	}
	return result, nil
}

func branchTitle(sourceTitle string) string {
	if sourceTitle == "" {
		return "Branch"
	}
	return "Branch: " + sourceTitle
}
