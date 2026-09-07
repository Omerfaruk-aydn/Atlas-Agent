package model

import (
	"image"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet/layout"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/logo"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// selectedLargeModel returns the currently selected large language model as
// memoized by the off-thread busy/agent probe (see workspace_cache.go), or
// nil when the agent isn't ready. It must never probe the workspace: it is
// called on every frame and AgentIsReady/AgentModel are synchronous HTTP
// round-trips in client/server mode.
func (m *UI) selectedLargeModel() *workspace.AgentModel {
	if m.agentReady {
		model := m.agentModel
		return &model
	}
	return nil
}

// landingView renders the landing page view showing the current working
// directory, model information, and LSP/MCP status in a two-column layout.
func (m *UI) landingView() string {
	t := m.com.Styles
	width := m.layout.main.Dx()
	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)

	parts := []string{
		cwd,
	}

	parts = append(parts, "", m.modelInfo(width))
	infoSection := lipgloss.JoinVertical(lipgloss.Left, parts...)

	var remainingHeightArea image.Rectangle
	layout.Vertical(
		layout.Len(lipgloss.Height(infoSection)+1),
		layout.Fill(1),
	).Split(m.layout.main).Assign(new(image.Rectangle), &remainingHeightArea)

	content := m.landingCards(width, max(1, remainingHeightArea.Dy()))

	return lipgloss.NewStyle().
		Width(width).
		Height(m.layout.main.Dy() - 1).
		PaddingTop(1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, infoSection, "", content),
		)
}

// Landing card layout.
const (
	// cardGutter is the blank columns between adjacent cards.
	cardGutter = 1
	// cardCount is how many resource cards sit in the row.
	cardCount = 3
	// cardChrome is the border and padding a card spends per side pair:
	// two border glyphs plus two padding columns.
	cardChrome = 4
	// cardBorderRows is the top and bottom edge a card spends vertically.
	cardBorderRows = 2
	// cardMaxWidth caps a single card so the row doesn't sprawl on very
	// wide terminals.
	cardMaxWidth = 34
	// cardMaxItems caps how many rows a card's list may use, independent
	// of terminal height. Without this, a section with hundreds of
	// entries (e.g. many discovered skills) grows to fill the whole
	// screen, and since every card in the row shares one height, the
	// other two -- often nearly empty -- get stretched to match, pushing
	// everything below the row down with them.
	cardMaxItems = 3
	// cardFrameDivisor halves the wordmark's 60fps tick so the card
	// borders sweep at 30fps.
	cardFrameDivisor = 2
)

// landingCards renders the LSP/MCP/Skills panels as a row of rounded cards
// whose borders carry the same rainbow sweep as the wordmark, at half its
// frame rate. Card contents keep their theme colors: the sweep frames the
// data rather than competing with it.
//
// The row is sized to the wordmark's width when the wordmark is on screen so
// the two line up on both edges; otherwise it fills the available width.
func (m *UI) landingCards(width, availHeight int) string {
	rowWidth := width
	if cols, _, ok := logo.BannerSize(width); ok {
		rowWidth = cols
	}
	rowWidth = min(rowWidth, cardCount*cardMaxWidth+(cardCount-1)*cardGutter)

	// Distribute the row width across the cards, giving the leftmost cards
	// the remainder so the row spans rowWidth exactly.
	inner := rowWidth - (cardCount-1)*cardGutter
	base, extra := inner/cardCount, inner%cardCount
	widths := make([]int, cardCount)
	for i := range widths {
		widths[i] = base
		if i < extra {
			widths[i]++
		}
	}
	if base <= cardChrome {
		// Too narrow to frame anything; fall back to the plain sections.
		return m.landingSectionsPlain(width, availHeight)
	}

	// Every card is as tall as the tallest, so the row's bottom edge is
	// straight. maxItems is what each list may render before it elides,
	// bounded above by cardMaxItems so one large list can't drag the
	// other two -- and the rest of the landing page -- down with it.
	maxItems := max(1, min(availHeight-cardBorderRows, cardMaxItems))
	titles := []string{"LSPs", "MCPs", "Skills"}
	bodies := []string{
		m.lspListing(widths[0]-cardChrome, maxItems),
		m.mcpListing(widths[1]-cardChrome, maxItems),
		m.skillsListing(widths[2]-cardChrome, maxItems),
	}

	bodyHeight := 0
	for _, b := range bodies {
		bodyHeight = max(bodyHeight, lipgloss.Height(b))
	}
	bodyHeight = min(bodyHeight, maxItems)

	t := m.com.Styles
	frame := m.bannerFrame / cardFrameDivisor
	cards := make([]string, 0, cardCount*2-1)
	xOffset := 0
	for i := range cardCount {
		if i > 0 {
			cards = append(cards, strings.Repeat(" ", cardGutter))
			xOffset += cardGutter
		}
		cards = append(cards, logo.RainbowBox(
			t.Resource.Heading.Render(titles[i]),
			bodies[i],
			widths[i],
			bodyHeight,
			frame,
			xOffset,
		))
		xOffset += widths[i]
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

// landingSectionsPlain is the unframed fallback for terminals too narrow to
// fit bordered cards.
func (m *UI) landingSectionsPlain(width, availHeight int) string {
	w := min(30, (width-2)/3)
	h := max(1, availHeight)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.lspInfo(w, h, false), " ",
		m.mcpInfo(w, h, false), " ",
		m.skillsInfo(w, h, false),
	)
}
