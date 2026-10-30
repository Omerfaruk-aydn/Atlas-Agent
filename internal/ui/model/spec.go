package model

import (
	"errors"
	"os"
	"path/filepath"

	editor "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-editor"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

type specLoadedMsg struct {
	snapshot workplan.SpecSnapshot
	err      error
}

type specChangedMsg struct{ err error }

func (m *UI) loadSpec() tea.Cmd {
	return func() tea.Msg {
		workingDir := m.com.Workspace.WorkingDir()
		plan, _, err := workplan.Load(workingDir)
		if err != nil {
			return specLoadedMsg{err: err}
		}
		if plan == nil {
			return specLoadedMsg{err: errors.New("create .atlas/work.json before opening specification review")}
		}
		snapshot, err := workplan.LoadSpec(workingDir)
		if err != nil {
			return specLoadedMsg{err: err}
		}
		for stage, content := range workplan.RenderSpec(plan) {
			if snapshot.Documents[string(stage)] == "" {
				if err := workplan.WriteSpecStage(workingDir, snapshot, stage, content); err != nil {
					return specLoadedMsg{err: err}
				}
			}
		}
		snapshot, err = workplan.LoadSpec(workingDir)
		return specLoadedMsg{snapshot: snapshot, err: err}
	}
}

func (m *UI) approveSpec(stage workplan.SpecStage, digest string) tea.Cmd {
	return func() tea.Msg {
		return specChangedMsg{err: workplan.ApproveSpecStage(m.com.Workspace.WorkingDir(), stage, digest)}
	}
}

func (m *UI) editSpec(stage workplan.SpecStage) tea.Cmd {
	workingDir := m.com.Workspace.WorkingDir()
	path := filepath.Join(workingDir, workplan.Dir, "spec", string(stage)+".md")
	cmd, err := editor.Command("atlas", path)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil || stage != workplan.SpecTasks {
			return specChangedMsg{err: err}
		}
		snapshot, loadErr := workplan.LoadSpec(workingDir)
		if loadErr != nil {
			return specChangedMsg{err: loadErr}
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return specChangedMsg{err: readErr}
		}
		return specChangedMsg{err: workplan.UpdateTasks(workingDir, snapshot.ContractDigest, string(data))}
	})
}
