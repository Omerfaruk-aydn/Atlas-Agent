package chat

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/anim"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
)

// SubmissionItem shows local progress before an assistant message exists.
// It is a display item only and never enters persisted model history.
type SubmissionItem struct {
	*list.Versioned
	id   string
	anim *anim.Anim
}

func NewSubmissionItem(sty *styles.Styles, id string) *SubmissionItem {
	return &SubmissionItem{
		Versioned: list.NewVersioned(),
		id:        id,
		anim: anim.New(anim.Settings{
			ID: id, Label: sty.Text("Waiting for response"), NoScramble: true,
			LabelColor: sty.WorkingLabelColor, Suffix: common.Elapsed,
			SuffixColor: sty.WorkingTimerColor,
		}),
	}
}

func (s *SubmissionItem) ID() string { return s.id }

func (s *SubmissionItem) Finished() bool { return false }

func (s *SubmissionItem) Render(width int) string { return s.RawRender(width) }

func (s *SubmissionItem) RawRender(width int) string {
	return ansi.Truncate(s.anim.Render(), max(0, width), "…")
}

func (s *SubmissionItem) StartAnimation() tea.Cmd { return s.anim.Start() }

func (s *SubmissionItem) Animate(msg anim.StepMsg) tea.Cmd {
	s.Bump()
	return s.anim.Animate(msg)
}
