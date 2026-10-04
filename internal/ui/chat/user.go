package chat

import (
	"bytes"
	"encoding/xml"
	stdimage "image"
	_ "image/gif"  // register GIF format
	_ "image/jpeg" // register JPEG format
	_ "image/png"  // register PNG format
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/anim"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/attachments"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	fimage "github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/image"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
)

// skillInvocation represents the XML structure for a loaded skill.
type skillInvocation struct {
	Name         string `xml:"name"`
	Description  string `xml:"description"`
	Location     string `xml:"location"`
	Instructions string `xml:"instructions"`
}

// UserMessageItem represents a user message in the chat UI.
type UserMessageItem struct {
	*list.Versioned
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	attachments *attachments.Renderer
	message     *message.Message
	sty         *styles.Styles

	// imgEnc/cellSize decide whether an attached image is drawn inline
	// below its chip. imgEnc is EncodingNone unless the terminal speaks
	// the Kitty graphics protocol, which is the only way to put real
	// pixels on the screen; everywhere else the chip's filename is all
	// that is shown.
	//
	// A half-block mosaic used to fill that gap, but its resolution is
	// the cell grid — a preview was 48x48 pixels at best, which reads as
	// a mosaic rather than a picture, so it was removed. Sixel is not
	// offered either: it paints at the cursor rather than into cells, so
	// it cannot survive a diff-based cell renderer.
	imgEnc   fimage.Encoding
	cellSize fimage.CellSize
	isTmux   bool
}

// NewUserMessageItem creates a new UserMessageItem. caps determines how
// attached images are rendered inline (see imgEnc).
func NewUserMessageItem(sty *styles.Styles, message *message.Message, attachments *attachments.Renderer, caps common.Capabilities) MessageItem {
	v := list.NewVersioned()
	imgEnc := fimage.EncodingNone
	if caps.SupportsKittyGraphics() {
		imgEnc = fimage.EncodingKitty
	}
	cellW, cellH := caps.CellSize()
	_, isTmux := caps.Env.LookupEnv("TMUX")
	return &UserMessageItem{
		Versioned:                v,
		highlightableMessageItem: defaultHighlighter(sty, v),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     newFocusableMessageItem(v),
		attachments:              attachments,
		message:                  message,
		sty:                      sty,
		imgEnc:                   imgEnc,
		cellSize:                 fimage.CellSize{Width: cellW, Height: cellH},
		isTmux:                   isTmux,
	}
}

// Finished implements list.Item. User messages are immutable once
// submitted, so the entry is always safe to freeze.
func (m *UserMessageItem) Finished() bool {
	return true
}

// RawRender implements [MessageItem].
func (m *UserMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)

	content, height, ok := m.getCachedRender(cappedWidth)
	// cache hit
	if ok {
		return m.renderHighlighted(content, cappedWidth, height)
	}

	msgContent := strings.TrimSpace(m.message.Content().Text)

	// Check if this is a skill invocation (loaded_skill XML)
	if strings.HasPrefix(msgContent, "<loaded_skill>") {
		content = m.renderSkillInvocation(msgContent, cappedWidth)
		height = lipgloss.Height(content)
		m.setCachedRender(content, cappedWidth, height)
		return m.renderHighlighted(content, cappedWidth, height)
	}

	renderer := common.UserMarkdownRenderer(m.sty, cappedWidth)
	mu := common.LockMarkdownRenderer(renderer)

	mu.Lock()
	result, err := renderer.Render(msgContent)
	mu.Unlock()

	if err != nil {
		content = msgContent
	} else {
		content = strings.TrimSuffix(result, "\n")
	}

	if len(m.message.BinaryContent()) > 0 {
		attachmentsStr := m.renderAttachments(cappedWidth)
		if content == "" {
			content = attachmentsStr
		} else {
			content = strings.Join([]string{content, "", attachmentsStr}, "\n")
		}
	}

	height = lipgloss.Height(content)
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

// renderSkillInvocation renders a loaded_skill XML as a special UI element.
func (m *UserMessageItem) renderSkillInvocation(content string, width int) string {
	var skill skillInvocation
	if err := xml.Unmarshal([]byte(content), &skill); err != nil {
		// If parsing fails, just render as markdown
		renderer := common.UserMarkdownRenderer(m.sty, width)
		mu := common.LockMarkdownRenderer(renderer)

		mu.Lock()
		result, err := renderer.Render(content)
		mu.Unlock()

		if err != nil {
			return content
		}
		return strings.TrimSuffix(result, "\n")
	}

	return toolOutputSkillContent(m.sty, skill.Name, skill.Description)
}

// Render implements MessageItem.
func (m *UserMessageItem) Render(width int) string {
	// Bypass the prefix cache while a highlight range is active so
	// selection drags reflect immediately without invalidating the
	// cache. Highlight changes are intentionally applied "above" the
	// prefix cache.
	useCache := !m.isHighlighted()
	var key uint64
	if m.focused {
		key = 1
	}
	if useCache {
		if cached, ok := m.getCachedPrefixedRender(width, key); ok {
			return cached
		}
	}
	var prefix string
	if m.focused {
		prefix = m.sty.Messages.UserFocused.Render()
	} else {
		prefix = m.sty.Messages.UserBlurred.Render()
	}
	lines := strings.Split(m.RawRender(width), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	out := strings.Join(lines, "\n")
	if useCache {
		m.setCachedPrefixedRender(out, width, key)
	}
	return out
}

// ID implements MessageItem.
func (m *UserMessageItem) ID() string {
	return m.message.ID
}

// imagePreviewCols/Rows size the Kitty graphics placement below an image
// attachment's chip. They are cells, not pixels: the terminal scales the
// transmitted image into that box.
const (
	imagePreviewCols = 48
	imagePreviewRows = 24
)

// renderAttachments renders the attachment chips, followed by an inline
// preview for any image attachment (a half-block mosaic by default, sharp
// Kitty graphics on supporting terminals — see imgEnc).
func (m *UserMessageItem) renderAttachments(width int) string {
	var atts []message.Attachment
	for _, at := range m.message.BinaryContent() {
		atts = append(atts, message.Attachment{
			FileName: at.Path,
			MimeType: at.MIMEType,
			Content:  at.Data,
		})
	}
	// This message is already posted, so the attachment can't be removed;
	// don't render the remove button.
	out := m.attachments.Render(atts, false, false, width)
	if previews := m.renderImagePreviews(atts); previews != "" {
		out = strings.Join([]string{out, previews}, "\n")
	}
	return out
}

// imagePreviewID returns the image cache key for the i-th attachment of
// this message. Scoped by message ID so two attachments that happen to
// share a filename (e.g. two clipboard pastes both named "paste_1.png")
// never collide in the package-level image cache.
func (m *UserMessageItem) imagePreviewID(i int) string {
	return m.message.ID + ":" + strconv.Itoa(i)
}

// StartAnimation implements Animatable. It has nothing to do with
// animation frames — it piggybacks on the same "run once right after the
// item is added to the list" hook every other message item already gets
// (see the StartAnimation call sites in model/ui.go) to transmit any image
// attachments to the terminal exactly once. Transmit's returned command is
// what actually performs I/O for Kitty graphics (writing the encoded image
// via a tea.RawMsg) or fills the block-art cache, so it must go through
// the real Bubbletea command pipeline rather than being invoked directly
// from Render, which cannot return commands.
func (m *UserMessageItem) StartAnimation() tea.Cmd {
	var cmds []tea.Cmd
	for i, at := range m.message.BinaryContent() {
		if !strings.HasPrefix(at.MIMEType, "image/") || len(at.Data) == 0 {
			continue
		}
		id := m.imagePreviewID(i)
		if fimage.HasTransmitted(id, imagePreviewCols, imagePreviewRows) {
			// Already cached from an earlier StartAnimation call (e.g. the
			// item scrolled out of view and back in) — skip the decode.
			continue
		}
		img, _, err := stdimage.Decode(bytes.NewReader(at.Data))
		if err != nil {
			continue // corrupt or unsupported format; the chip still shows the filename
		}
		if cmd := m.imgEnc.Transmit(id, img, m.cellSize, imagePreviewCols, imagePreviewRows, m.isTmux); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	// The first RawRender (drawn before Transmit's async command lands)
	// cached its output with no preview, keyed only by width — with no
	// notion of "the image cache just became warm". Clear it and bump
	// the F6 list-cache version so the next Draw actually recomputes
	// and picks up the now-available preview instead of serving the
	// stale cached string.
	return tea.Sequence(tea.Batch(cmds...), func() tea.Msg {
		m.clearCache()
		m.Bump()
		return nil
	})
}

// Animate implements Animatable. Image transmission is one-shot (see
// StartAnimation) and never establishes an ongoing tick chain, so there is
// nothing to advance on a spinner step.
func (m *UserMessageItem) Animate(anim.StepMsg) tea.Cmd {
	return nil
}

// renderImagePreviews renders the cached preview for each image attachment
// that StartAnimation successfully transmitted. Attachments that failed to
// decode, or haven't been transmitted yet (image cache not warm on the
// very first frame), are silently skipped — the chip above still shows the
// filename either way.
func (m *UserMessageItem) renderImagePreviews(atts []message.Attachment) string {
	var previews []string
	for i, at := range atts {
		if !at.IsImage() {
			continue
		}
		if r := m.imgEnc.Render(m.imagePreviewID(i), imagePreviewCols, imagePreviewRows); r != "" {
			previews = append(previews, r)
		}
	}
	return strings.Join(previews, "\n\n")
}

// HandleKeyEvent implements KeyEventHandler.
func (m *UserMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := m.message.Content().Text
		return true, common.CopyToClipboard(text, m.sty.Text("Message copied to clipboard"))
	}
	return false, nil
}
