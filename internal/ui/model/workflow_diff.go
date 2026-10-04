package model

import (
	"path/filepath"
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
)

func (m *UI) refreshWorkflowDiff() {
	if m.dialog == nil {
		return
	}
	d, ok := m.dialog.Dialog(dialog.FileDiffID).(*dialog.FileDiff)
	if !ok {
		return
	}
	for _, file := range m.sessionFiles {
		d.Refresh(dialog.FileDiffEntry{Path: file.LatestVersion.Path, Before: file.FirstVersion.Content, After: file.LatestVersion.Content, Additions: file.Additions, Deletions: file.Deletions})
	}
}

func (m *UI) beginWorkflowFeedback(file string, line int) tea.Cmd {
	if m.session == nil {
		return nil
	}
	root := m.com.Workspace.WorkingDir()
	if filepath.IsAbs(file) {
		relative, err := filepath.Rel(root, file)
		if err != nil {
			m.workflow.err = err.Error()
			return nil
		}
		file = relative
	}
	file = filepath.ToSlash(file)
	if file == ".." || strings.HasPrefix(file, "../") {
		m.workflow.err = m.com.Text("Review file is outside this project")
		return nil
	}
	task := ""
	if row, ok := m.workflow.selectedRow(); ok {
		task = row.TaskID
	}
	if task == "" {
		for _, candidate := range m.session.Todos {
			if session.OwnershipOverlaps(candidate.OwnedPaths, []string{file}) && candidate.Status != session.TodoStatusCompleted {
				if task != "" {
					task = ""
					break
				}
				task = candidate.ID
			}
		}
	}
	m.workflow.input = workflowInput{mode: "feedback", task: task, file: file, line: line}
	if !m.workflow.open {
		return m.openWorkflowPanel()
	}
	return nil
}
