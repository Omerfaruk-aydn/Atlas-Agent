package common

import (
	"fmt"
	"image"
	"os"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clipboard"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

// MaxAttachmentSize defines the maximum allowed size for file attachments (5 MB).
const MaxAttachmentSize = int64(5 * 1024 * 1024)

// AllowedImageTypes defines the permitted image file types.
var AllowedImageTypes = []string{".jpg", ".jpeg", ".png"}

// Common defines common UI options and configurations.
type Common struct {
	Workspace workspace.Workspace
	Styles    *styles.Styles
}

// Config returns the pure-data configuration associated with this [Common] instance.
func (c *Common) Config() *config.Config {
	return c.Workspace.Config()
}

// DefaultCommon returns the default common UI configurations. When the
// workspace has a large model selected, the theme is chosen based on its
// provider; otherwise the default theme is used.
func DefaultCommon(ws workspace.Workspace) *Common {
	// Corner style for every framed surface, settled before the theme is
	// built. The theme bakes the corners into its border styles, so an
	// explicit config choice has to land first or the dialogs would keep the
	// auto-detected corners while the composer used the configured ones.
	if ws != nil {
		if cfg := ws.Config(); cfg != nil && cfg.Options != nil && cfg.Options.TUI != nil {
			styles.SetBoxCorners(cfg.Options.TUI.BoxCorners)
		}
	}
	s := styles.ThemeForProvider(largeModelProviderID(ws))
	s.Locale = i18n.New("en")
	if ws != nil {
		if cfg := ws.Config(); cfg != nil && cfg.Options != nil && cfg.Options.TUI != nil {
			s.Locale.Set(cfg.Options.TUI.Language)
		}
	}
	return &Common{
		Workspace: ws,
		Styles:    &s,
	}
}

// Text translates an Atlas-authored literal without touching user content.
func (c *Common) Text(source string) string {
	if c == nil {
		return source
	}
	return c.Styles.Text(source)
}

// largeModelProviderID returns the provider ID of the currently selected
// large model, or the empty string if none is set or the workspace is nil.
func largeModelProviderID(ws workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	cfg := ws.Config()
	if cfg == nil {
		return ""
	}
	return cfg.Models[config.SelectedModelTypeLarge].Provider
}

// IsHyper reported whether the currently selected large model was provided
// by Hyper. The Hyper provider has been removed; callers should be updated.
func (c *Common) IsHyper() bool {
	return false
}

// CenterRect returns a new [Rectangle] centered within the given area with the
// specified width and height.
func CenterRect(area uv.Rectangle, width, height int) uv.Rectangle {
	centerX := area.Min.X + area.Dx()/2
	centerY := area.Min.Y + area.Dy()/2
	minX := centerX - width/2
	minY := centerY - height/2
	maxX := minX + width
	maxY := minY + height
	return image.Rect(minX, minY, maxX, maxY)
}

// BottomLeftRect returns a new [Rectangle] positioned at the bottom-left within the given area with the
// specified width and height.
func BottomLeftRect(area uv.Rectangle, width, height int) uv.Rectangle {
	minX := area.Min.X
	maxX := minX + width
	maxY := area.Max.Y
	minY := maxY - height
	return image.Rect(minX, minY, maxX, maxY)
}

// IsFileTooBig checks if the file at the given path exceeds the specified size
// limit.
func IsFileTooBig(filePath string, sizeLimit int64) (bool, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("error getting file info: %w", err)
	}

	if fileInfo.Size() > sizeLimit {
		return true, nil
	}

	return false, nil
}

// CopyToClipboard copies the given text to the clipboard using both OSC 52
// (terminal escape sequence) and native clipboard for maximum compatibility.
// Returns a command that reports success to the user with the given message.
func CopyToClipboard(text, successMessage string) tea.Cmd {
	return CopyToClipboardWithCallback(text, successMessage, nil)
}

// CopyToClipboardWithCallback copies text to clipboard and executes a callback
// before showing the success message.
// This is useful when you need to perform additional actions like clearing UI state.
func CopyToClipboardWithCallback(text, successMessage string, callback tea.Cmd) tea.Cmd {
	return tea.Sequence(
		tea.SetClipboard(text),
		func() tea.Msg {
			clipboard.WriteText(text)
			return nil
		},
		callback,
		util.ReportInfo(successMessage),
	)
}
