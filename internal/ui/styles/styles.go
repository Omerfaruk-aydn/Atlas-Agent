// Package styles define styling and theming for the project.
package styles

import (
	"fmt"
	"image/color"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-markdown/v2/ansi"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-style/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/filepicker"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textarea"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/diffview"
	"github.com/alecthomas/chroma/v2"
)

const (
	CheckIcon       string = "✓"
	SpinnerIcon     string = "⋯"
	LoadingIcon     string = "⟳"
	ModelIcon       string = "◇"
	HypercreditIcon string = "◆"

	ArrowRightIcon string = "→"

	ToolPending string = "●"
	ToolSuccess string = "✓"
	ToolError   string = "×"

	RadioOn  string = "◉"
	RadioOff string = "○"

	BorderThin  string = "│"
	BorderThick string = "▌"

	SectionSeparator string = "─"

	BoxHorizontal string = "─"
	BoxVertical   string = "│"

	TodoCompletedIcon  string = "✓"
	TodoPendingIcon    string = "•"
	TodoInProgressIcon string = "→"

	ImageIcon  string = "▣"
	TextIcon   string = "≡"
	SkillIcon  string = "▲"
	RemoveIcon string = "✕"

	ScrollbarThumb string = "┃"
	ScrollbarTrack string = "│"

	LSPErrorIcon   string = "E"
	LSPWarningIcon string = "W"
	LSPInfoIcon    string = "I"
	LSPHintIcon    string = "H"
)

// BoxCornerStyle names a corner set for framed surfaces such as the composer
// and the landing cards. Which of these actually render is a property of the
// terminal font, not of the terminal: a font missing a glyph draws a
// replacement box ("tofu") in its place. They are offered as a choice because
// no single set is universally available — notably the rounded set lives at
// U+256D–U+2570, which plenty of fonts omit.
type BoxCornerStyle = string

const (
	// CornerSharp is in the basic Box Drawing block and renders essentially
	// everywhere, which is why it is the default.
	CornerSharp BoxCornerStyle = "sharp"
	// CornerRounded is the true rounded set (U+256D–U+2570).
	CornerRounded BoxCornerStyle = "rounded"
	// CornerArc approximates rounded corners with quadrant arcs from the
	// Geometric Shapes block (U+25DC–U+25DF), which some fonts carry even
	// when they lack the rounded box-drawing set.
	CornerArc BoxCornerStyle = "arc"
	// CornerBold and CornerDouble are heavier variants from the same Box
	// Drawing block as CornerSharp, so they travel about as well.
	CornerBold   BoxCornerStyle = "bold"
	CornerDouble BoxCornerStyle = "double"
	// CornerBevel chamfers the corners with plain ASCII slashes. It is the
	// only non-square style that cannot fail to render: every other curved
	// or rounded set depends on glyphs a font may simply not carry, while
	// "/" and "\" are ASCII and therefore always present.
	CornerBevel BoxCornerStyle = "bevel"
)

// boxCorners maps each style to its glyphs, in the order
// top-left, top-right, bottom-left, bottom-right.
var boxCorners = map[BoxCornerStyle][4]string{
	CornerSharp:   {"┌", "┐", "└", "┘"},
	CornerRounded: {"╭", "╮", "╰", "╯"},
	CornerArc:     {"◜", "◝", "◟", "◞"},
	CornerBold:    {"┏", "┓", "┗", "┛"},
	CornerDouble:  {"╔", "╗", "╚", "╝"},
	CornerBevel:   {"/", `\`, `\`, "/"},
}

// DetectCornerStyle picks the corner style to use when the config doesn't
// name one, so users don't have to know what a codepoint is to get a
// good-looking frame.
//
// There is no way to ask a terminal whether its font carries a glyph: a
// missing glyph is drawn as a replacement box but still reports the same
// cursor advance as a real one, so it can't be probed. What can be checked is
// which terminal is hosting us, which is a good proxy: every mainstream
// terminal but the legacy Windows console ships with a font carrying the
// rounded box-drawing set, and the legacy console is the case that renders
// them as boxes. So rounded is the default, and the legacy console falls back
// to the ASCII bevel, which cannot fail to render anywhere.
func DetectCornerStyle() BoxCornerStyle {
	if runtime.GOOS != "windows" {
		return CornerRounded
	}
	// Any modern host on Windows announces itself in the environment. The
	// legacy console announces nothing, which is exactly what identifies it.
	for _, key := range []string{
		"WT_SESSION",         // Windows Terminal
		"ConEmuPID",          // ConEmu / Cmder
		"WEZTERM_EXECUTABLE", // WezTerm
		"ALACRITTY_LOG",      // Alacritty
		"TERM_PROGRAM",       // VS Code, Ghostty, Hyper, Tabby, …
		"MSYSTEM",            // Git Bash / MSYS2
	} {
		if os.Getenv(key) != "" {
			return CornerRounded
		}
	}
	return CornerBevel
}

// Corner glyphs for framed surfaces. These are variables rather than
// constants because the style is configurable; see SetBoxCorners.
//
// They start out at whatever DetectCornerStyle picks so that code paths which
// never reach SetBoxCorners — the exit banner, `atlas run`, anything built
// before the config is known — still get corners suited to the terminal
// instead of a fixed fallback.
var BoxTopLeft, BoxTopRight, BoxBottomLeft, BoxBottomRight = detectedCorners()

func detectedCorners() (string, string, string, string) {
	c := boxCorners[DetectCornerStyle()]
	return c[0], c[1], c[2], c[3]
}

// SetBoxCorners selects the corner style used by every framed surface. An
// empty or unknown style defers to DetectCornerStyle, so an unset config
// means "pick something sensible for this terminal" rather than a fixed
// choice. It is called once during startup, before the UI draws anything, so
// the glyphs are effectively read-only afterwards.
func SetBoxCorners(style BoxCornerStyle) {
	c, ok := boxCorners[style]
	if !ok {
		c = boxCorners[DetectCornerStyle()]
	}
	BoxTopLeft, BoxTopRight, BoxBottomLeft, BoxBottomRight = c[0], c[1], c[2], c[3]
}

// Border returns the single-line border used by every framed surface that
// draws through lipgloss — dialogs, pills, the compact details panel. The
// edges are always the thin Box Drawing pair; only the corners follow the
// active style. Call it at style-build time rather than caching a package
// value, so a theme rebuilt after SetBoxCorners picks up the new corners.
//
// This exists so those surfaces cannot drift from the ones that draw their
// frames by hand (the composer, the landing cards): a user whose font lacks
// the rounded set would otherwise get a clean composer and boxed-out dialogs.
func Border() lipgloss.Border {
	b := lipgloss.RoundedBorder()
	b.TopLeft, b.TopRight = BoxTopLeft, BoxTopRight
	b.BottomLeft, b.BottomRight = BoxBottomLeft, BoxBottomRight
	return b
}

// UVBorder is [Border] for surfaces drawn through Ultraviolet, such as the
// question-form tabs.
func UVBorder() uv.Border {
	b := uv.RoundedBorder()
	b.TopLeft.Content, b.TopRight.Content = BoxTopLeft, BoxTopRight
	b.BottomLeft.Content, b.BottomRight.Content = BoxBottomLeft, BoxBottomRight
	return b
}

// BoxCornerStyles returns the available corner style names, sorted, for
// callers that need to present or validate the choice.
func BoxCornerStyles() []string {
	names := make([]string, 0, len(boxCorners))
	for name := range boxCorners {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

const (
	defaultMargin     = 2
	defaultListIndent = 2
)

type Styles struct {
	// ANSI holds the 16 standard ANSI colors (0-7 normal, 8-15 bright)
	// used to remap legible colors onto raw terminal output, such as the
	// output of bang-mode shell commands. Terminal programs emit the
	// basic 16-color SGR codes (red, green, blue, …) and leave the actual
	// colors up to the terminal; without this palette they fall through
	// to the user's terminal defaults, which are often illegible on
	// ATLAS-AGENT's background. Defining them here keeps output readable and
	// on-brand regardless of terminal configuration.
	ANSI [16]color.Color

	// Header
	Header struct {
		Atlas             lipgloss.Style // Style for "Atlas™" label
		Percentage        lipgloss.Style // Style for context percentage
		HypercreditIcon   lipgloss.Style // Style for Hypercredit count (◆ N)
		Keystroke         lipgloss.Style // Style for keystroke hints (e.g., "ctrl+d")
		KeystrokeTip      lipgloss.Style // Style for keystroke action text (e.g., "open", "close")
		WorkingDir        lipgloss.Style // Style for current working directory
		Separator         lipgloss.Style // Style for separator dots (•)
		Wrapper           lipgloss.Style // Outer container for the entire header row
		LogoGradCanvas    lipgloss.Style // Canvas for the compact "ATLAS-AGENT" gradient
		LogoGradFromColor color.Color    // "ATLAS-AGENT" wordmark gradient start
		LogoGradToColor   color.Color    // "ATLAS-AGENT" wordmark gradient end
	}

	CompactDetails struct {
		View    lipgloss.Style
		Version lipgloss.Style
		Title   lipgloss.Style
	}

	// Tool calls
	ToolCallSuccess lipgloss.Style

	// Text selection
	TextSelection lipgloss.Style

	// Markdown & Chroma
	Markdown      ansi.StyleConfig
	QuietMarkdown ansi.StyleConfig

	// Inputs
	TextInput textinput.Styles

	// Help
	Help help.Styles

	// Diff
	Diff diffview.Style

	// FilePicker
	FilePicker filepicker.Styles

	// Buttons
	Button struct {
		Focused  lipgloss.Style
		Blurred  lipgloss.Style
		Hovered  lipgloss.Style
		Negative lipgloss.Style // Selected negative/destructive action.
	}

	// Editor
	Editor struct {
		Textarea textarea.Styles

		// ComposerAccent is the composer frame's color in the default
		// (manual) permission mode. The other modes borrow the color of
		// their prompt dots, which carries their signal; manual has no
		// such signal of its own, so it takes the theme's frame color —
		// the same one the dialogs are bordered with.
		ComposerAccent color.Color

		// Normal mode prompt (default "::: ").
		PromptNormalFocused lipgloss.Style
		PromptNormalBlurred lipgloss.Style

		// YOLO mode prompt (" ! " icon + ":::" dots).
		PromptYoloIconFocused lipgloss.Style
		PromptYoloIconBlurred lipgloss.Style
		PromptYoloDotsFocused lipgloss.Style
		PromptYoloDotsBlurred lipgloss.Style

		// Bang mode prompt (" ! " icon + ":::" dots, Turtle color).
		PromptBangIconFocused lipgloss.Style
		PromptBangIconBlurred lipgloss.Style
		PromptBangDotsFocused lipgloss.Style
		PromptBangDotsBlurred lipgloss.Style

		// Plan mode prompt (read-only permission mode).
		PromptPlanIconFocused lipgloss.Style
		PromptPlanIconBlurred lipgloss.Style
		PromptPlanDotsFocused lipgloss.Style
		PromptPlanDotsBlurred lipgloss.Style

		// Auto-accept-edits mode prompt.
		PromptAutoAcceptIconFocused lipgloss.Style
		PromptAutoAcceptIconBlurred lipgloss.Style
		PromptAutoAcceptDotsFocused lipgloss.Style
		PromptAutoAcceptDotsBlurred lipgloss.Style

		// Question mode prompt (" ? " icon + ":::" dots).
		PromptQuestionIconFocused lipgloss.Style
		PromptQuestionIconBlurred lipgloss.Style

		// Question choice styling.
		QuestionSelected   lipgloss.Style // Active choice text (Dolly).
		QuestionUnselected lipgloss.Style // Inactive header text (Sash).
		QuestionBody       lipgloss.Style // Description/body text.
		QuestionConfirm    lipgloss.Style // Confirm tab title (primary).
		QuestionNote       lipgloss.Style // Saved note text (dimmer than body).
		QuestionCursorBar  lipgloss.Style // Active cursor indicator bar.
		QuestionRadioOn    lipgloss.Style // Selected single-choice radio.
		QuestionRadioOff   lipgloss.Style // Unselected single-choice radio.
		QuestionCheckOn    lipgloss.Style // Checked multi-choice indicator.
		QuestionCheckOff   lipgloss.Style // Unchecked multi-choice indicator.
	}

	// Radio
	Radio struct {
		On    lipgloss.Style
		Off   lipgloss.Style
		Label lipgloss.Style // Text next to a radio button
	}

	// Tabs for batch question forms. Uses uv types for direct
	// screen rendering without lipgloss.
	Tab struct {
		ActiveBorder          uv.Border
		InactiveBorder        uv.Border
		ActiveBorderBlurred   uv.Border
		InactiveBorderBlurred uv.Border
		ActiveStyle           uv.Style
		InactiveStyle         uv.Style
	}

	// Background
	Background color.Color

	// Logo
	Logo struct {
		FieldColor         color.Color
		TitleColorA        color.Color
		TitleColorB        color.Color
		AtlasColor         color.Color
		VersionColor       color.Color
		SmallAtlas         lipgloss.Style // "Atlas™" label in SmallRender
		SmallDiagonals     lipgloss.Style // Diagonal line fill in SmallRender
		GradCanvas         lipgloss.Style // Blank canvas for gradient painting
		SmallGradFromColor color.Color    // Small "ATLAS-AGENT" wordmark gradient start
		SmallGradToColor   color.Color    // Small "ATLAS-AGENT" wordmark gradient end
	}

	// Working indicator gradient (spinners/shimmers on assistant "thinking",
	// tool-call pending, CLI generating, startup).
	WorkingGradFromColor color.Color
	WorkingGradToColor   color.Color
	WorkingLabelColor    color.Color // Label text color next to the indicator
	WorkingTimerColor    color.Color // Elapsed timer suffix color

	// Section Title
	Section struct {
		Title lipgloss.Style
		Line  lipgloss.Style
	}

	// Initialize
	Initialize struct {
		Header  lipgloss.Style
		Content lipgloss.Style
		Accent  lipgloss.Style
	}

	// LSP
	LSP struct {
		ErrorDiagnostic   lipgloss.Style
		WarningDiagnostic lipgloss.Style
		HintDiagnostic    lipgloss.Style
		InfoDiagnostic    lipgloss.Style
	}

	// Sidebar
	Sidebar struct {
		SessionTitle lipgloss.Style // Current session title at top of sidebar
		WorkingDir   lipgloss.Style // Working directory path (PrettyPath)
	}

	// ModelInfo (model name, provider, reasoning, token/cost summary)
	ModelInfo struct {
		Icon                 lipgloss.Style // Model icon (◇)
		Name                 lipgloss.Style // Model name text
		Provider             lipgloss.Style // "via <provider>" text
		ProviderFallback     lipgloss.Style // Provider on its own second line
		Reasoning            lipgloss.Style // Reasoning effort text
		TokenCount           lipgloss.Style // "(42K)" token count
		TokenPercentage      lipgloss.Style // "42%" percent of context window
		EstimatedUsagePrefix lipgloss.Style // "~" prefix for estimated usage
		Cost                 lipgloss.Style // "$0.42" cost readout
		HypercreditIcon      lipgloss.Style // Hypercredit icon (◆)
		HypercreditText      lipgloss.Style // Remaining Hypercredits text
	}

	// Resource styles the LSP/MCP/skills sidebar lists: their heading,
	// each row's status icon, name, status text, and truncation hints.
	Resource struct {
		Heading         lipgloss.Style // Section header ("LSPs", "MCPs", "Skills")
		Name            lipgloss.Style // Resource name (e.g. "gopls")
		StatusText      lipgloss.Style // Row status description (e.g. "starting...")
		OfflineIcon     lipgloss.Style // Offline/unstarted/stopped status icon
		DisabledIcon    lipgloss.Style // Disabled status icon
		BusyIcon        lipgloss.Style // Busy/starting status icon
		ErrorIcon       lipgloss.Style // Error status icon
		OnlineIcon      lipgloss.Style // Online/ready status icon
		NeedsAuthIcon   lipgloss.Style // Needs authentication status icon
		AdditionalText  lipgloss.Style // "None" and "…and N more" text
		CapabilityCount lipgloss.Style // "N tools" / "N prompts" / "N resources"
		RowTitleBase    lipgloss.Style // Base style applied over row titles in common.Status
		RowDescBase     lipgloss.Style // Base style applied over row descriptions in common.Status
		DefaultTitleFg  color.Color    // Default title color when opt is zero
		DefaultDescFg   color.Color    // Default description color when opt is zero
	}

	// Files
	Files struct {
		Path           lipgloss.Style
		Additions      lipgloss.Style
		Deletions      lipgloss.Style
		SectionTitle   lipgloss.Style // "Modified Files" heading
		EmptyMessage   lipgloss.Style // "None" placeholder when no files
		TruncationHint lipgloss.Style // "…and N more" message
	}

	// Chat
	// Messages - chat message item styles
	Messages struct {
		UserBlurred      lipgloss.Style
		UserFocused      lipgloss.Style
		AssistantBlurred lipgloss.Style
		AssistantFocused lipgloss.Style
		NoContent        lipgloss.Style
		Thinking         lipgloss.Style
		ErrorTag         lipgloss.Style
		ErrorTitle       lipgloss.Style
		ErrorDetails     lipgloss.Style
		ToolCallFocused  lipgloss.Style
		ToolCallCompact  lipgloss.Style
		ToolCallBlurred  lipgloss.Style

		// Shell (bang mode) item styles.
		ShellBarFocused    lipgloss.Style // Left vertical bar when focused.
		ShellBarBlurred    lipgloss.Style // Left vertical bar when blurred.
		ShellPrompt        lipgloss.Style // "$" prompt symbol (focused).
		ShellPromptBlurred lipgloss.Style // "$" prompt symbol (blurred).
		ShellCommand       lipgloss.Style // Command text (syntax-highlighted).
		ShellOutput        lipgloss.Style // Plain output text.
		ShellExitCode      lipgloss.Style // Non-zero exit code indicator.
		ShellTruncation    lipgloss.Style // "N more lines" hint.
		SectionHeader      lipgloss.Style

		// Thinking section styles
		ThinkingBox            lipgloss.Style // Background for thinking content
		ThinkingTruncationHint lipgloss.Style // "… (N lines hidden)" hint
		ThinkingFooterTitle    lipgloss.Style // "Thought for" text
		ThinkingFooterDuration lipgloss.Style // Duration value
		AssistantInfoIcon      lipgloss.Style
		AssistantInfoModel     lipgloss.Style
		AssistantInfoProvider  lipgloss.Style
		AssistantInfoDuration  lipgloss.Style
		AssistantCanceled      lipgloss.Style // Italic "Canceled" footer
	}

	// Tool - styles for tool call rendering
	Tool struct {
		// Icon styles with tool status
		IconPending   lipgloss.Style
		IconSuccess   lipgloss.Style
		IconError     lipgloss.Style
		IconCancelled lipgloss.Style

		// Tool name styles
		NameNormal lipgloss.Style // Top-level tool name
		NameNested lipgloss.Style // Nested child tool name (inside Agent/Agentic Fetch)

		// Parameter list styles
		ParamMain lipgloss.Style
		ParamKey  lipgloss.Style

		// Content rendering styles
		ContentLine           lipgloss.Style // Individual content line with background and width
		ContentTruncation     lipgloss.Style // Truncation message "… (N lines)"
		ContentCodeLine       lipgloss.Style // Code line with background and width
		ContentCodeTruncation lipgloss.Style // Code truncation message with bgBase
		ContentCodeBg         color.Color    // Background color for syntax highlighting
		Body                  lipgloss.Style // Body content padding (PaddingLeft(2))

		// Deprecated - kept for backward compatibility
		ContentBg         lipgloss.Style // Content background
		ContentText       lipgloss.Style // Content text
		ContentLineNumber lipgloss.Style // Line numbers in code

		// State message styles
		StateWaiting   lipgloss.Style // "Waiting for tool response..."
		StateCancelled lipgloss.Style // "Canceled."

		// Error styles
		ErrorTag     lipgloss.Style // ERROR tag
		ErrorMessage lipgloss.Style // Error message text

		// Warning styles (used for permission denied)
		WarnTag     lipgloss.Style // WARN tag
		WarnMessage lipgloss.Style // Warning message text

		// Diff styles
		DiffTruncation lipgloss.Style // Diff truncation message with padding

		// Multi-edit note styles
		NoteTag     lipgloss.Style // NOTE tag (yellow background)
		NoteMessage lipgloss.Style // Note message text

		// Job header styles (for bash jobs)
		JobIconPending lipgloss.Style // Pending job icon (green dark)
		JobIconError   lipgloss.Style // Error job icon (red dark)
		JobIconSuccess lipgloss.Style // Success job icon (green)
		JobToolName    lipgloss.Style // Job tool name "Bash" (blue)
		JobAction      lipgloss.Style // Action text (Start, Output, Kill)
		JobPID         lipgloss.Style // PID text
		JobDescription lipgloss.Style // Description text

		// Agent task styles
		AgentTaskTag lipgloss.Style // Agent task tag (blue background, bold)
		AgentPrompt  lipgloss.Style // Agent prompt text

		// Agentic fetch styles
		AgenticFetchPromptTag lipgloss.Style // Agentic fetch prompt tag (green background, bold)

		// Todo styles
		TodoRatio          lipgloss.Style // Todo ratio (e.g., "2/5")
		TodoCompletedIcon  lipgloss.Style // Completed todo icon
		TodoInProgressIcon lipgloss.Style // In-progress todo icon
		TodoPendingIcon    lipgloss.Style // Pending todo icon
		TodoStatusNote     lipgloss.Style // " · completed N" / " · starting task" trailing note
		TodoItem           lipgloss.Style // Default body text for todo list items
		TodoJustStarted    lipgloss.Style // Text of the just-started todo in tool-call bodies

		// MCP tools
		MCPName     lipgloss.Style // The mcp name
		MCPToolName lipgloss.Style // The mcp tool name
		MCPArrow    lipgloss.Style // The mcp arrow icon

		// Images and external resources
		ResourceLoadedText      lipgloss.Style
		ResourceLoadedIndicator lipgloss.Style
		ResourceName            lipgloss.Style
		ResourceSize            lipgloss.Style
		MediaType               lipgloss.Style

		// Hooks
		HookLabel        lipgloss.Style // "Hook" label
		HookName         lipgloss.Style // Hook command name
		HookMatcher      lipgloss.Style // Matcher regex pattern
		HookArrow        lipgloss.Style // Arrow indicator
		HookDetail       lipgloss.Style // Decision detail text
		HookOK           lipgloss.Style // "OK" status
		HookDenied       lipgloss.Style // "Denied" status
		HookDeniedLabel  lipgloss.Style // "Hook" label when denied
		HookDeniedReason lipgloss.Style // Denied reason text
		HookRewrote      lipgloss.Style // "Rewrote Input" indicator

		// Action verb colors for tool-call headers.
		ActionCreate  lipgloss.Style // Constructive actions (e.g. "Add", "Create")
		ActionDestroy lipgloss.Style // Destructive actions (e.g. "Remove", "Delete")

		// Tool result helpers.
		ResultEmpty      lipgloss.Style // "No results" placeholder
		ResultTruncation lipgloss.Style // "… and N more" truncation line
		ResultItemName   lipgloss.Style // Item name (left column in result lists)
		ResultItemDesc   lipgloss.Style // Item description (right column)
	}

	// Dialog styles
	Dialog struct {
		Title       lipgloss.Style
		TitleText   lipgloss.Style
		TitleError  lipgloss.Style
		TitleAccent lipgloss.Style
		// View is the main content area style.
		View          lipgloss.Style
		PrimaryText   lipgloss.Style
		SecondaryText lipgloss.Style
		// HelpView is the line that contains the help.
		HelpView lipgloss.Style
		Help     struct {
			Ellipsis       lipgloss.Style
			ShortKey       lipgloss.Style
			ShortDesc      lipgloss.Style
			ShortSeparator lipgloss.Style
			FullKey        lipgloss.Style
			FullDesc       lipgloss.Style
			FullSeparator  lipgloss.Style
		}

		NormalItem   lipgloss.Style
		SelectedItem lipgloss.Style
		InputPrompt  lipgloss.Style

		List lipgloss.Style

		Spinner lipgloss.Style

		// ContentPanel is used for content blocks with subtle background.
		ContentPanel   lipgloss.Style
		ContentPanelBg color.Color // Background color for ContentPanel syntax highlighting.

		// Scrollbar styles for scrollable content.
		ScrollbarThumb lipgloss.Style
		ScrollbarTrack lipgloss.Style

		// Arguments
		Arguments struct {
			Content                  lipgloss.Style
			Description              lipgloss.Style
			InputLabelBlurred        lipgloss.Style
			InputLabelFocused        lipgloss.Style
			InputRequiredMarkBlurred lipgloss.Style
			InputRequiredMarkFocused lipgloss.Style
		}

		// ListItem styles the info-text rendered alongside list items (commands,
		// models, reasoning options). Sessions have their own overrides below.
		ListItem struct {
			InfoBlurred lipgloss.Style
			InfoFocused lipgloss.Style
		}

		Models struct {
			ConfiguredText lipgloss.Style // "Configured" badge shown on the ModelGroup header
		}

		Permissions struct {
			KeyText   lipgloss.Style // Left key cell of a key/value row
			ValueText lipgloss.Style // Right value cell of a key/value row
			ParamsBg  color.Color    // Background color behind highlighted JSON parameters
		}

		Quit struct {
			Content lipgloss.Style // Wrapper for the quit dialog's inner content
			Hint    lipgloss.Style // Style for quit hint
			Frame   lipgloss.Style // Outer rounded border framing the quit dialog
		}

		APIKey struct {
			Spinner lipgloss.Style // Loading spinner while validating the key
		}

		OAuth struct {
			Spinner      lipgloss.Style // Loading spinner
			Instructions lipgloss.Style // Emphasized instruction text
			UserCode     lipgloss.Style // Prominent user code display
			Success      lipgloss.Style // Positive status text (e.g. "Authentication successful!")
			Link         lipgloss.Style // Underlined verification URL
			Enter        lipgloss.Style // "enter" keyword highlight in instructions
			ErrorText    lipgloss.Style // Error message when authentication fails
			StatusText   lipgloss.Style // Narrative status text ("Initializing...", "Verifying...", etc.)
			UserCodeBg   color.Color    // Background color of the centered user-code box
		}

		ImagePreview lipgloss.Style

		Sessions struct {
			// styles for when we are in delete mode
			DeletingView        lipgloss.Style
			DeletingItemFocused lipgloss.Style
			DeletingItemBlurred lipgloss.Style
			DeletingTitle       lipgloss.Style
			DeletingMessage     lipgloss.Style

			// styles for when we are in update mode
			RenamingView           lipgloss.Style
			RenamingingItemFocused lipgloss.Style
			RenamingItemBlurred    lipgloss.Style
			RenamingingTitle       lipgloss.Style
			RenamingingMessage     lipgloss.Style
			RenamingPlaceholder    lipgloss.Style

			InfoBlurred lipgloss.Style // Timestamp text on unfocused session items
			InfoFocused lipgloss.Style // Timestamp text on the focused session item
		}
	}

	// Status bar and help
	Status struct {
		Help lipgloss.Style

		ErrorIndicator   lipgloss.Style
		WarnIndicator    lipgloss.Style
		InfoIndicator    lipgloss.Style
		UpdateIndicator  lipgloss.Style
		SuccessIndicator lipgloss.Style

		ErrorMessage   lipgloss.Style
		WarnMessage    lipgloss.Style
		InfoMessage    lipgloss.Style
		UpdateMessage  lipgloss.Style
		SuccessMessage lipgloss.Style
	}

	// Completions popup styles
	Completions struct {
		Normal  lipgloss.Style
		Focused lipgloss.Style
		Match   lipgloss.Style
		// Info styles the trailing hint column -- the keyboard
		// shortcut on a slash command. It has to sit on the same
		// background as the row it trails, so callers re-background
		// it per row rather than relying on the value here.
		Info lipgloss.Style
	}

	// Attachments styles
	Attachments struct {
		Normal   lipgloss.Style
		Image    lipgloss.Style
		Text     lipgloss.Style
		Skill    lipgloss.Style
		Remove   lipgloss.Style
		Deleting lipgloss.Style
	}

	// Pills styles for todo/queue pills
	Pills struct {
		Base               lipgloss.Style // Base pill style with padding
		Focused            lipgloss.Style // Pill with visible rounded border
		QueueItemPrefix    lipgloss.Style // Prefix for queue list items
		QueueItemText      lipgloss.Style // Queue list item body text
		QueueLabel         lipgloss.Style // "N Queued" label text
		QueueIconBase      lipgloss.Style // Base style for queue gradient triangles
		QueueGradFromColor color.Color    // Start color for queue indicator gradient
		QueueGradToColor   color.Color    // End color for queue indicator gradient
		TodoLabel          lipgloss.Style // "To-Do" label
		TodoProgress       lipgloss.Style // Todo ratio (e.g. "2/5")
		TodoCurrentTask    lipgloss.Style // Current in-progress task name
		TodoSpinner        lipgloss.Style // Todo spinner style
		HelpKey            lipgloss.Style // Keystroke hint style
		HelpText           lipgloss.Style // Help action text style
		Area               lipgloss.Style // Pills area container
	}
}

// ChromaTheme converts the current markdown chroma styles to a chroma
// StyleEntries map.
func (s *Styles) ChromaTheme() chroma.StyleEntries {
	rules := s.Markdown.CodeBlock

	return chroma.StyleEntries{
		chroma.Text:                chromaStyle(rules.Chroma.Text),
		chroma.Error:               chromaStyle(rules.Chroma.Error),
		chroma.Comment:             chromaStyle(rules.Chroma.Comment),
		chroma.CommentPreproc:      chromaStyle(rules.Chroma.CommentPreproc),
		chroma.Keyword:             chromaStyle(rules.Chroma.Keyword),
		chroma.KeywordReserved:     chromaStyle(rules.Chroma.KeywordReserved),
		chroma.KeywordNamespace:    chromaStyle(rules.Chroma.KeywordNamespace),
		chroma.KeywordType:         chromaStyle(rules.Chroma.KeywordType),
		chroma.Operator:            chromaStyle(rules.Chroma.Operator),
		chroma.Punctuation:         chromaStyle(rules.Chroma.Punctuation),
		chroma.Name:                chromaStyle(rules.Chroma.Name),
		chroma.NameBuiltin:         chromaStyle(rules.Chroma.NameBuiltin),
		chroma.NameTag:             chromaStyle(rules.Chroma.NameTag),
		chroma.NameAttribute:       chromaStyle(rules.Chroma.NameAttribute),
		chroma.NameClass:           chromaStyle(rules.Chroma.NameClass),
		chroma.NameConstant:        chromaStyle(rules.Chroma.NameConstant),
		chroma.NameDecorator:       chromaStyle(rules.Chroma.NameDecorator),
		chroma.NameException:       chromaStyle(rules.Chroma.NameException),
		chroma.NameFunction:        chromaStyle(rules.Chroma.NameFunction),
		chroma.NameOther:           chromaStyle(rules.Chroma.NameOther),
		chroma.Literal:             chromaStyle(rules.Chroma.Literal),
		chroma.LiteralNumber:       chromaStyle(rules.Chroma.LiteralNumber),
		chroma.LiteralDate:         chromaStyle(rules.Chroma.LiteralDate),
		chroma.LiteralString:       chromaStyle(rules.Chroma.LiteralString),
		chroma.LiteralStringEscape: chromaStyle(rules.Chroma.LiteralStringEscape),
		chroma.GenericDeleted:      chromaStyle(rules.Chroma.GenericDeleted),
		chroma.GenericEmph:         chromaStyle(rules.Chroma.GenericEmph),
		chroma.GenericInserted:     chromaStyle(rules.Chroma.GenericInserted),
		chroma.GenericStrong:       chromaStyle(rules.Chroma.GenericStrong),
		chroma.GenericSubheading:   chromaStyle(rules.Chroma.GenericSubheading),
		chroma.Background:          chromaStyle(rules.Chroma.Background),
	}
}

// DialogHelpStyles returns the styles for dialog help.
func (s *Styles) DialogHelpStyles() help.Styles {
	return help.Styles(s.Dialog.Help)
}

// hex returns a pointer to the "#rrggbb" representation of c. It's used to
// satisfy glamour's string-pointer API when configuring markdown colors
// from the theme palette.
func hex(c color.Color) *string {
	r, g, b, _ := c.RGBA()
	s := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	return &s
}

func chromaStyle(style ansi.StylePrimitive) string {
	var s strings.Builder

	if style.Color != nil {
		s.WriteString(*style.Color)
	}
	if style.BackgroundColor != nil {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("bg:")
		s.WriteString(*style.BackgroundColor)
	}
	if style.Italic != nil && *style.Italic {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("italic")
	}
	if style.Bold != nil && *style.Bold {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("bold")
	}
	if style.Underline != nil && *style.Underline {
		if s.Len() > 0 {
			s.WriteString(" ")
		}
		s.WriteString("underline")
	}

	return s.String()
}
