// Package rewind lets the user jump back to an earlier point in a
// conversation and have the working directory's files restored to their
// state as of that point, without losing anything: the target session is
// never modified or deleted, only forked.
package rewind

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

// Result is what ForkAt produced: the new session plus a summary of the
// disk changes it made, so callers can show the user what happened.
type Result struct {
	Session      session.Session
	FilesWritten int
	FilesDeleted int
}

// Service forks a session at a chosen user message and restores the
// working directory's files to their state before it.
type Service interface {
	// Preview reports how many files ForkAt would write and delete for the
	// given checkpoint, without creating a session or touching any file on
	// disk. Files already in their restored state are not counted. Used to
	// show an accurate confirmation before the (hard to reverse) apply.
	Preview(ctx context.Context, sourceSessionID, messageID string) (filesToWrite, filesToDelete int, err error)

	// ForkAt creates a new session containing a copy of sourceSessionID's
	// messages up to and including messageID, so the conversation ends
	// with that message, and restores the working directory's files to
	// their content before it: what the agent changed in answer to it and
	// later is undone. Nothing is run; the user decides what to send next.
	// The file history up to that point is carried over, so the new session
	// can be rewound in turn. sourceSessionID itself is never modified or
	// deleted: this is non-destructive by design.
	ForkAt(ctx context.Context, sourceSessionID, messageID string) (Result, error)
}

type service struct {
	sessions session.Service
	messages message.Service
	files    history.Service
}

func NewService(sessions session.Service, messages message.Service, files history.Service) Service {
	return &service{sessions: sessions, messages: messages, files: files}
}

// resolve returns the ordered messages in sourceSessionID up to and
// including messageID and the resolved file state before it. Shared by
// Preview (read-only) and ForkAt (applies the result).
func (s *service) resolve(ctx context.Context, sourceSessionID, messageID string) ([]message.Message, []history.ResolvedFile, error) {
	// message.Service.List flushes nothing itself; callers that need the
	// latest debounced state must flush first (mirrors the session-switch
	// read pattern documented on message.Service).
	if err := s.messages.FlushAll(ctx); err != nil {
		return nil, nil, fmt.Errorf("rewind: flushing pending message state: %w", err)
	}

	all, err := s.messages.List(ctx, sourceSessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("rewind: listing messages: %w", err)
	}

	targetIdx := -1
	for i, m := range all {
		if m.ID == messageID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return nil, nil, fmt.Errorf("rewind: message %q not found in session %q", messageID, sourceSessionID)
	}
	kept := all[:targetIdx+1]

	before := all[:targetIdx]
	ids := make([]string, len(before))
	for i, m := range before {
		ids[i] = m.ID
	}
	resolved, err := s.files.ResolveAsOf(ctx, sourceSessionID, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("rewind: resolving file state: %w", err)
	}

	return kept, resolved, nil
}

func (s *service) Preview(ctx context.Context, sourceSessionID, messageID string) (int, int, error) {
	_, resolved, err := s.resolve(ctx, sourceSessionID, messageID)
	if err != nil {
		return 0, 0, err
	}
	written, deleted := 0, 0
	for _, rf := range resolved {
		switch change(rf) {
		case changeWrite:
			written++
		case changeDelete:
			deleted++
		}
	}
	return written, deleted, nil
}

func (s *service) ForkAt(ctx context.Context, sourceSessionID, messageID string) (Result, error) {
	source, err := s.sessions.Get(ctx, sourceSessionID)
	if err != nil {
		return Result{}, fmt.Errorf("rewind: loading source session: %w", err)
	}

	kept, resolved, err := s.resolve(ctx, sourceSessionID, messageID)
	if err != nil {
		return Result{}, err
	}

	// The fork is an ordinary top-level session: a child session is
	// treated as a sub-agent's and is left out of the session list.
	fork, err := s.sessions.Create(ctx, rewindTitle(source.Title))
	if err != nil {
		return Result{}, fmt.Errorf("rewind: creating forked session: %w", err)
	}
	fork, err = s.copyHistory(ctx, source, fork, kept)
	if err != nil {
		// Nothing on disk has changed yet; drop the half-built fork.
		_ = s.sessions.Delete(context.WithoutCancel(ctx), fork.ID)
		return Result{}, err
	}

	result := Result{Session: fork}
	for _, rf := range resolved {
		switch change(rf) {
		case changeDelete:
			if err := os.Remove(rf.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return result, fmt.Errorf("rewind: removing %q: %w", rf.Path, err)
			}
			result.FilesDeleted++
		case changeWrite:
			if err := os.MkdirAll(filepath.Dir(rf.Path), 0o755); err != nil {
				return result, fmt.Errorf("rewind: creating parent dir for %q: %w", rf.Path, err)
			}
			if err := os.WriteFile(rf.Path, []byte(*rf.Content), 0o644); err != nil {
				return result, fmt.Errorf("rewind: writing %q: %w", rf.Path, err)
			}
			result.FilesWritten++
		}
	}

	return result, nil
}

// copyHistory copies the kept messages into fork, then the file versions
// those messages produced along with the pre-session baselines, so that
// rewinding the fork later restores files too. Message IDs are new in the
// fork, so file versions and the summary pointer are mapped onto them.
func (s *service) copyHistory(ctx context.Context, source, fork session.Session, kept []message.Message) (session.Session, error) {
	ids := make(map[string]string, len(kept))
	summaryID := ""
	for _, m := range kept {
		copied, err := s.messages.Create(ctx, fork.ID, message.CreateMessageParams{
			Role:             m.Role,
			Parts:            copyParts(m),
			Model:            m.Model,
			Provider:         m.Provider,
			IsSummaryMessage: m.IsSummaryMessage,
		})
		if err != nil {
			return fork, fmt.Errorf("rewind: copying message %q: %w", m.ID, err)
		}
		ids[m.ID] = copied.ID
		if m.ID == source.SummaryMessageID || m.IsSummaryMessage {
			summaryID = copied.ID
		}
	}

	versions, err := s.files.ListBySession(ctx, source.ID)
	if err != nil {
		return fork, fmt.Errorf("rewind: listing file history: %w", err)
	}
	// Versions come oldest first, so they are recreated in order.
	for _, v := range versions {
		messageID, copied := ids[v.MessageID]
		switch {
		case v.Version == history.InitialVersion && v.MessageID == "":
			_, err = s.files.Create(ctx, fork.ID, v.Path, v.Content, "")
		case !copied:
			// Produced after the checkpoint, or drift recorded without a
			// message, which rewinding never restores.
			continue
		case v.Version == history.InitialVersion:
			_, err = s.files.Create(ctx, fork.ID, v.Path, v.Content, messageID)
		default:
			_, err = s.files.CreateVersion(ctx, fork.ID, v.Path, v.Content, messageID)
		}
		if err != nil {
			return fork, fmt.Errorf("rewind: copying history of %q: %w", v.Path, err)
		}
	}

	if summaryID != "" {
		// Without the pointer the agent would send the conversation from
		// before the summary again instead of starting at the summary.
		fork.SummaryMessageID = summaryID
		saved, err := s.sessions.Save(ctx, fork)
		if err != nil {
			return fork, fmt.Errorf("rewind: saving forked session: %w", err)
		}
		fork = saved
	}
	return fork, nil
}

// copyParts returns m's parts for a copy. Create adds a finish mark to
// every message but an assistant's, so the one m already has is left out
// rather than doubled on each rewind.
func copyParts(m message.Message) []message.ContentPart {
	if m.Role == message.Assistant {
		return m.Parts
	}
	parts := make([]message.ContentPart, 0, len(m.Parts))
	for _, part := range m.Parts {
		if _, finish := part.(message.Finish); !finish {
			parts = append(parts, part)
		}
	}
	return parts
}

type fileChange int

const (
	changeNone fileChange = iota
	changeWrite
	changeDelete
)

// change reports what restoring rf would do to the file on disk; a file
// already in its restored state needs nothing.
func change(rf history.ResolvedFile) fileChange {
	current, err := os.ReadFile(rf.Path)
	exists := err == nil
	if rf.Content == nil {
		if exists || !errors.Is(err, fs.ErrNotExist) {
			return changeDelete
		}
		return changeNone
	}
	if exists && bytes.Equal(current, []byte(*rf.Content)) {
		return changeNone
	}
	return changeWrite
}

func rewindTitle(sourceTitle string) string {
	if sourceTitle == "" {
		return "Rewind"
	}
	return "Rewind: " + sourceTitle
}
