package chat

import (
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestViewRendersLegacySkillAddressAsAtlas(t *testing.T) {
	t.Parallel()
	sty := styles.AtlasPantera()
	renderer := &ViewToolRenderContext{}
	rendered := renderer.RenderTool(&sty, 120, &ToolRenderOpts{ToolCall: message.ToolCall{Name: "view", Input: `{"file_path":"crush://skills/desktop-automation/SKILL.md"}`, Finished: true}, Status: ToolStatusSuccess, Compact: true})
	require.Contains(t, rendered, "atlas://skills/desktop-automation/SKILL.md")
	require.NotContains(t, rendered, "crush://")
}
