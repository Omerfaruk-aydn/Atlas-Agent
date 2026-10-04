package model

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/diff"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/history"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type workflowFilesLoaded struct {
	openRequested bool
	epoch         uint64
	sessionID     string
	entries       []dialog.FileDiffEntry
	err           error
}

func (m *UI) workflowFilesCmd() tea.Cmd {
	if m.session == nil || m.workflow.diffLoading {
		return nil
	}
	m.workflow.diffLoading = true
	id, epoch, ws := m.session.ID, m.workflow.epoch, m.com.Workspace
	openRequested := !m.dialog.ContainsDialog(dialog.FilesID) && !m.dialog.ContainsDialog(dialog.FileDiffID)
	known := []string{id}
	for _, run := range m.workflow.snapshot.Runners {
		known = append(known, run.SessionID)
	}
	for i := len(m.workflow.snapshot.Executions) - 1; i >= 0; i-- {
		known = append(known, m.workflow.snapshot.Executions[i].SessionIDs...)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		children := ws.AgentHubEntries(ctx, id)
		slices.SortStableFunc(children, func(a, b workspace.AgentHubEntry) int { return b.StartedAt.Compare(a.StartedAt) })
		for _, child := range children {
			known = append(known, child.SessionID)
		}
		seen := map[string]bool{}
		files := map[string][]history.File{}
		omitted := map[string]bool{}
		total := 0
		var firstErr error
		root := ws.WorkingDir()
		for _, sid := range known {
			if seen[sid] {
				continue
			}
			if len(seen) >= 32 {
				firstErr = fmt.Errorf("additional agent histories omitted; diff includes the coordinator and up to 31 recent/live children")
				break
			}
			seen[sid] = true
			records, err := ws.ListSessionHistory(ctx, sid)
			if err != nil {
				firstErr = err
				continue
			}
			for _, file := range records {
				if omitted[file.Path] {
					continue
				}
				rel, err := filepath.Rel(root, file.Path)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					continue
				}
				if len(file.Content) > 1024*1024 || total+len(file.Content) > 8*1024*1024 {
					firstErr = errors.New(m.com.Text("large file histories omitted from the bounded diff view"))
					for _, previous := range files[file.Path] {
						total -= len(previous.Content)
					}
					delete(files, file.Path)
					omitted[file.Path] = true
					continue
				}
				if len(files) >= 256 && files[file.Path] == nil {
					firstErr = errors.New(m.com.Text("diff view is limited to 256 files"))
					continue
				}
				total += len(file.Content)
				files[file.Path] = append(files[file.Path], file)
			}
		}
		var entries []dialog.FileDiffEntry
		for path, versions := range files {
			slices.SortStableFunc(versions, func(a, b history.File) int {
				if a.CreatedAt < b.CreatedAt {
					return -1
				}
				if a.CreatedAt > b.CreatedAt {
					return 1
				}
				if a.Version < b.Version {
					return -1
				}
				if a.Version > b.Version {
					return 1
				}
				return 0
			})
			first, last := versions[0], versions[len(versions)-1]
			_, additions, deletions := diff.GenerateDiff(first.Content, last.Content, path)
			if additions == 0 && deletions == 0 {
				continue
			}
			entries = append(entries, dialog.FileDiffEntry{Path: path, Before: first.Content, After: last.Content, Additions: additions, Deletions: deletions})
		}
		slices.SortFunc(entries, func(a, b dialog.FileDiffEntry) int { return strings.Compare(a.Path, b.Path) })
		return workflowFilesLoaded{epoch: epoch, sessionID: id, entries: entries, err: firstErr, openRequested: openRequested}
	}
}
