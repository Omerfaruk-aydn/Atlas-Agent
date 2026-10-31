package dialog

import (
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secretprompt"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestSecretPromptMasksInputAndHandsTheValueToTheCallback(t *testing.T) {
	sty := styles.AtlasPantera()
	d := NewSecretPrompt(&common.Common{Styles: &sty}, secretprompt.Request{ID: "r1", Title: "API key for acme", Detail: "Sent only to host."})

	var gotID, gotSecret string
	d.OnSubmit = func(id, secret string) { gotID, gotSecret = id, secret }

	const secret = "sk-dialog-SECRET-42"
	for _, r := range secret {
		d.HandleMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	scr := uv.NewScreenBuffer(80, 24)
	d.Draw(scr, uv.Rect(0, 0, 80, 24))
	require.NotContains(t, scr.String(), secret, "what is typed is never rendered")
	require.Contains(t, scr.String(), "API key for acme")

	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.IsType(t, ActionClose{}, action)
	require.Equal(t, "r1", gotID)
	require.Equal(t, secret, gotSecret)
}

func TestSecretPromptEscCancelsWithoutASecret(t *testing.T) {
	sty := styles.AtlasPantera()
	d := NewSecretPrompt(&common.Common{Styles: &sty}, secretprompt.Request{ID: "r2", Title: "API key"})
	var cancelled string
	d.OnSubmit = func(string, string) { t.Fatal("must not submit") }
	d.OnCancel = func(id string) { cancelled = id }

	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.IsType(t, ActionClose{}, action)
	require.Equal(t, "r2", cancelled)
}
