package dialog

import (
	"strings"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
)

// LanguagesID identifies the persistent interface language picker.
const LanguagesID = "languages"

// ActionSelectLanguage requests asynchronous persistence by the UI owner.
type ActionSelectLanguage struct{ Code string }

// Languages provides native language names, filtering, and keyboard selection.
type Languages struct {
	com      *common.Common
	input    textinput.Model
	selected int
	saving   bool
}

func NewLanguages(com *common.Common) *Languages {
	input := textinput.New()
	input.SetVirtualCursor(false)
	input.SetStyles(com.Styles.TextInput)
	input.Placeholder = com.Text("Type to filter")
	input.Focus()
	l := &Languages{com: com, input: input}
	for index, lang := range i18n.Languages() {
		if lang.Code == com.Styles.Language() {
			l.selected = index
		}
	}
	return l
}

func (l *Languages) ID() string { return LanguagesID }

func (l *Languages) SetSaving(saving bool) { l.saving = saving }

func (l *Languages) choices() []i18n.Language {
	query := strings.ToLower(strings.TrimSpace(l.input.Value()))
	var choices []i18n.Language
	for _, language := range i18n.Languages() {
		if strings.Contains(strings.ToLower(language.Name+" "+language.Code), query) {
			choices = append(choices, language)
		}
	}
	return choices
}

func (l *Languages) HandleMsg(msg tea.Msg) Action {
	if l.saving {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		choices := l.choices()
		switch keyMsg.String() {
		case "esc", "alt+esc":
			return ActionClose{}
		case "up", "ctrl+p":
			if len(choices) > 0 {
				l.selected = (l.selected + len(choices) - 1) % len(choices)
			}
			return nil
		case "down", "ctrl+n":
			if len(choices) > 0 {
				l.selected = (l.selected + 1) % len(choices)
			}
			return nil
		case "enter":
			if len(choices) > 0 {
				l.saving = true
				return ActionSelectLanguage{Code: choices[l.selected].Code}
			}
			return nil
		}
	}
	previous := l.input.Value()
	var cmd tea.Cmd
	l.input, cmd = l.input.Update(msg)
	if previous != l.input.Value() {
		l.selected = 0
	}
	return ActionCmd{Cmd: cmd}
}

func (l *Languages) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := l.com.Styles
	width := max(0, min(64, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	l.input.SetWidth(dialogInputTextWidth(t, l.input, inner))
	rc := NewRenderContext(t, width)
	rc.Title = l.com.Text("Language")
	rc.AddPart(t.Dialog.InputPrompt.Render(l.input.View()))
	if l.saving {
		rc.AddPart(l.com.Text("Saving language…"))
	} else {
		choices := l.choices()
		rows := min(len(choices), max(0, area.Dy()-8))
		start := max(0, l.selected-rows+1)
		for index := start; index < min(len(choices), start+rows); index++ {
			language := choices[index]
			label := language.Name + " (" + language.Code + ")"
			if language.Code == t.Language() {
				label += " · " + l.com.Text("Current")
			}
			style := t.Dialog.NormalItem
			if index == l.selected {
				style = t.Dialog.SelectedItem
			}
			rc.AddPart(style.Width(inner).Render(label))
		}
	}
	rc.AddPart("↑/↓ " + l.com.Text("choose") + "   enter " + l.com.Text("confirm") + "   esc " + l.com.Text("cancel"))
	cur := InputCursor(t, l.input.Cursor())
	if l.saving {
		cur = nil
	}
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}
