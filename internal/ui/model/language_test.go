package model

import (
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textarea"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestLanguagePersistenceAndFailure(t *testing.T) {
	t.Parallel()
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("en")
	ws := &modelManagementWorkspace{cfg: &config.Config{Options: &config.Options{}}}
	m := &UI{com: &common.Common{Workspace: ws, Styles: &sty}, dialog: dialog.NewOverlay(), keyMap: DefaultKeyMap()}
	m.textarea = textarea.New()
	m.textarea.SetValue("Keep /language and D:\\Atlas unchanged")
	cmd := m.saveLanguage("tr")
	require.Empty(t, ws.setKey, "Persistence must run off the update loop")
	require.Equal(t, "en", sty.Language())
	result := cmd().(languageSavedMsg)
	require.NoError(t, result.err)
	require.Equal(t, "options.tui.language", ws.setKey)
	require.Equal(t, "tr", ws.setValue)
	m.handleLanguageSaved(result)
	require.Equal(t, "tr", sty.Language())
	require.Equal(t, "Keep /language and D:\\Atlas unchanged", m.textarea.Value())
	require.Equal(t, "çıkış", m.keyMap.Quit.Help().Desc)
	ws.setErr = errors.New("disk full")
	result = m.saveLanguage("ar")().(languageSavedMsg)
	require.Error(t, result.err)
	m.handleLanguageSaved(result)
	require.Equal(t, "tr", sty.Language(), "Failed persistence must not apply the locale")
}

func TestLanguageCommandPreservesIDsAndCustomContent(t *testing.T) {
	t.Parallel()
	sty := styles.AtlasPantera()
	sty.Locale = i18n.New("tr")
	ws := &modelManagementWorkspace{cfg: &config.Config{Options: &config.Options{}}}
	m := &UI{com: &common.Common{Workspace: ws, Styles: &sty}, dialog: dialog.NewOverlay()}
	items := m.slashCommandItems()
	for _, item := range items {
		if item.Name == "/language" {
			require.Equal(t, "Arayüz dilini seç", item.Detail)
			require.Equal(t, dialog.ActionOpenDialog{DialogID: dialog.LanguagesID}, item.Action)
			return
		}
	}
	t.Fatal("Language command missing")
}
