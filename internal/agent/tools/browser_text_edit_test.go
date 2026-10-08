package tools

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

func TestCanvasInsertDirectionalEmptyEditor(t *testing.T) {
	t.Parallel()
	for _, initial := range []string{"", "\u2068\u2069", "\u200e\u2068\u2069\u200f"} {
		t.Run(initial, func(t *testing.T) {
			t.Parallel()
			state := t.Name()
			ledgerFor(state).setTextEdit(&canvasTextEdit{tab: "tab", doc: "doc", editor: "proxy", initial: initial})
			defer ledgerFor(state).setTextEdit(nil)
			info := browser.FocusInfo{CanvasEditor: true, Key: "proxy", Text: initial}
			exec := browser.ExecutionIdentity{TabID: "tab", Document: "doc"}
			require.Nil(t, canvasInsertGuard(state, info, exec, ""))
		})
	}
}

func TestCanvasInsertDirectionalExistingTextStillProtected(t *testing.T) {
	t.Parallel()
	for _, initial := range []string{"\u2068ATLAS\u2069", "\u2067مرحبا\u2069", " ", "\u200d", "\u200c"} {
		t.Run(initial, func(t *testing.T) {
			t.Parallel()
			state := t.Name()
			ledgerFor(state).setTextEdit(&canvasTextEdit{tab: "tab", doc: "doc", editor: "proxy", initial: initial})
			defer ledgerFor(state).setTextEdit(nil)
			info := browser.FocusInfo{CanvasEditor: true, Key: "proxy", Text: initial}
			exec := browser.ExecutionIdentity{TabID: "tab", Document: "doc"}
			err := canvasInsertGuard(state, info, exec, "")
			require.NotNil(t, err)
			require.Contains(t, err.Error(), "text_editor_not_empty")
			require.Nil(t, canvasInsertGuard(state, info, exec, "append"))
		})
	}
}
