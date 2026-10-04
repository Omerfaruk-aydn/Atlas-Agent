package dialog

import (
	"image"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestLanguagePickerNativeFilterAndSelection(t *testing.T) {
	t.Parallel()
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	l := NewLanguages(&common.Common{Styles: &sty})
	require.Equal(t, 1, l.selected)
	l.input.SetValue("العربية")
	l.selected = 0
	require.Len(t, l.choices(), 1)
	action := l.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionSelectLanguage{Code: "ar"}, action)
	require.True(t, l.saving)
	require.Nil(t, l.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "No duplicate writes")
	l.SetSaving(false)
	l.input.SetValue("unknown")
	require.Nil(t, l.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

func TestLanguageBuiltinRoleCoverageAndCustomDescriptions(t *testing.T) {
	t.Parallel()
	for _, mode := range subagents.Builtin() {
		for _, language := range i18n.Languages() {
			require.True(t, i18n.Has(language.Code, firstSentence(mode.Description)), "%s: %s", language.Code, mode.Name)
		}
	}
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	entry := &subagentEntry{t: &common.Common{Styles: &sty}, sub: subagents.Subagent{Description: "Language", Model: "openai/model-id"}}
	require.Equal(t, "Language · openai/model-id", entry.info())
}

func TestLanguageCommandsKeepAliasesAndUserContent(t *testing.T) {
	t.Parallel()
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	com := &common.Common{Styles: &sty, Workspace: &modelRolesWorkspace{cfg: &config.Config{Options: &config.Options{SessionMode: "architect"}}}}
	d, err := NewCommands(com, "s1", true, false, true, true,
		[]commands.CustomCommand{{ID: "mine", Name: "Language", Content: "Keep /language and D:\\Atlas unchanged"}}, nil)
	require.NoError(t, err)
	sty.Locale.Set("en")
	original := make(map[string]*CommandItem)
	for _, item := range d.defaultCommands() {
		original[item.id] = item
	}
	sty.Locale.Set("tr")
	for _, item := range d.defaultCommands() {
		if original[item.id].summary != "" {
			require.True(t, i18n.Has("tr", original[item.id].summary), "Untranslated summary for %s", item.id)
			require.Equal(t, i18n.Text("tr", original[item.id].summary), item.summary)
		}
		if item.id == "language" {
			require.Equal(t, "Dil", item.title)
			require.Equal(t, "/language", item.slash)
			require.Contains(t, item.aliases, "Language")
		}
		if item.id == "session-mode" {
			require.Equal(t, "Sohbet modu (Architect)", item.title)
		}
	}
	custom := d.customCommandItems()
	require.Equal(t, "Language", custom[0].title)
	action := custom[0].action.(ActionRunCustomCommand)
	require.Equal(t, "Keep /language and D:\\Atlas unchanged", action.Content)
}

func TestLanguagePickerDrawsEveryLocaleAndTerminalSize(t *testing.T) {
	t.Parallel()
	for _, language := range i18n.Languages() {
		for _, size := range []image.Point{{X: 80, Y: 24}, {X: 40, Y: 12}, {X: 18, Y: 8}, {X: 0, Y: 0}} {
			sty := styles.AtlasPantera()
			sty.Locale = i18n.New(language.Code)
			picker := NewLanguages(&common.Common{Styles: &sty})
			screen := uv.NewScreenBuffer(size.X, size.Y)
			require.NotPanics(t, func() { picker.Draw(screen, image.Rectangle{Max: size}) })
			if size.X == 80 {
				require.Contains(t, screen.String(), i18n.Text(language.Code, "Language"))
				require.Contains(t, screen.String(), language.Name)
			}
			picker.SetSaving(true)
			require.Nil(t, picker.Draw(screen, image.Rectangle{Max: size}))
		}
	}
}
