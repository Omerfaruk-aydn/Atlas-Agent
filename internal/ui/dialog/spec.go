package dialog

import (
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

const SpecID = "spec"

type ActionSpecApprove struct {
	Stage  workplan.SpecStage
	Digest string
}

type (
	ActionSpecEdit   struct{ Stage workplan.SpecStage }
	ActionSpecReject struct{ Stage workplan.SpecStage }
)

// Spec presents the ordered human approval workflow. File I/O remains in the
// owning UI command, not in message handling or rendering.
type Spec struct {
	com      *common.Common
	snapshot workplan.SpecSnapshot
	selected int
}

func NewSpec(com *common.Common, snapshot workplan.SpecSnapshot) *Spec {
	return &Spec{com: com, snapshot: snapshot}
}

func (s *Spec) ID() string { return SpecID }

func (s *Spec) stage() workplan.SpecStage {
	return []workplan.SpecStage{workplan.SpecRequirements, workplan.SpecDesign, workplan.SpecTasks}[s.selected]
}

func (s *Spec) HandleMsg(msg tea.Msg) Action {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "esc", "alt+esc":
		return ActionClose{}
	case "up", "ctrl+p", "left":
		s.selected = (s.selected + 2) % 3
	case "down", "ctrl+n", "right", "tab":
		s.selected = (s.selected + 1) % 3
	case "enter":
		stage := s.stage()
		if digest := s.snapshot.Documents[string(stage)]; digest != "" {
			return ActionSpecApprove{Stage: stage, Digest: digest}
		}
	case "e":
		return ActionSpecEdit{Stage: s.stage()}
	case "r":
		return ActionSpecReject{Stage: s.stage()}
	}
	return nil
}

func (s *Spec) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := s.com.Styles
	width := max(0, min(68, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	rc := NewRenderContext(t, width)
	rc.Title = s.com.Text("Specification Review")
	stages := []workplan.SpecStage{workplan.SpecRequirements, workplan.SpecDesign, workplan.SpecTasks}
	for i, stage := range stages {
		status := "not generated"
		if digest := s.snapshot.Documents[string(stage)]; digest != "" {
			status = "awaiting approval"
			if s.snapshot.Approved[stage] == digest {
				status = "approved"
			}
		}
		style := t.Dialog.NormalItem
		if i == s.selected {
			style = t.Dialog.SelectedItem
		}
		rc.AddPart(style.Width(inner).Render(string(stage) + " · " + s.com.Text(status)))
	}
	rc.AddPart("enter " + s.com.Text("approve") + "   e " + s.com.Text("edit") + "   r " + s.com.Text("reject") + "   esc " + s.com.Text("close"))
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}
