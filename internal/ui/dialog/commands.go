package dialog

import (
	"os"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	uv "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ultraviolet"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/help"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/spinner"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/textinput"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/list"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/styles"
)

// CommandsID is the identifier for the commands dialog.
const CommandsID = "commands"

// CommandType represents the type of commands being displayed.
type CommandType uint

// String returns the string representation of the CommandType.
func (c CommandType) String() string { return []string{"System", "User", "MCP"}[c] }

const (
	sidebarCompactModeBreakpoint = 120
)

const (
	SystemCommands CommandType = iota
	UserCommands
	MCPPrompts
)

// Commands represents a dialog that shows available commands.
type dockerMCPAvailabilityCheckedMsg struct {
	available bool
}

type Commands struct {
	com    *common.Common
	keyMap struct {
		Select,
		UpDown,
		Next,
		Previous,
		Tab,
		ShiftTab,
		Close key.Binding
	}

	sessionID string
	// hasPreviousSession reports whether the user arrived here from
	// another session -- viewing a sub-agent's run from the jobs dialog
	// or the agent hub -- and so has somewhere to go back to.
	hasPreviousSession bool
	hasSession         bool
	hasTodos           bool
	hasQueue           bool
	selected           CommandType

	spinner spinner.Model
	loading bool

	help  help.Model
	input textinput.Model
	list  *list.FilterableList

	windowWidth int

	customCommands []commands.CustomCommand
	mcpPrompts     []commands.MCPPrompt

	dockerMCPAvailable     *bool
	dockerMCPCheckInFlight bool
}

var _ Dialog = (*Commands)(nil)

// NewCommands creates a new commands dialog.
func NewCommands(com *common.Common, sessionID string, hasSession, hasPreviousSession, hasTodos, hasQueue bool, customCommands []commands.CustomCommand, mcpPrompts []commands.MCPPrompt) (*Commands, error) {
	c := &Commands{
		com:                com,
		selected:           SystemCommands,
		sessionID:          sessionID,
		hasSession:         hasSession,
		hasPreviousSession: hasPreviousSession,
		hasTodos:           hasTodos,
		hasQueue:           hasQueue,
		customCommands:     customCommands,
		mcpPrompts:         mcpPrompts,
	}

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()

	c.help = help

	c.list = list.NewFilterableList()
	c.list.Focus()
	c.list.SetSelected(0)

	c.input = textinput.New()
	c.input.SetVirtualCursor(false)
	c.input.Placeholder = "Type to filter"
	c.input.SetStyles(com.Styles.TextInput)
	c.input.Focus()

	c.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	c.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	c.keyMap.Next = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next item"),
	)
	c.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	c.keyMap.Tab = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch selection"),
	)
	c.keyMap.ShiftTab = key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "switch selection prev"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	c.keyMap.Close = closeKey

	if available, known := config.DockerMCPAvailabilityCached(); known {
		c.dockerMCPAvailable = &available
	}

	// Set initial commands
	c.setCommandItems(c.selected)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = com.Styles.Dialog.Spinner
	c.spinner = s

	return c, nil
}

// ID implements Dialog.
func (c *Commands) ID() string {
	return CommandsID
}

// HandleMsg implements [Dialog].
func (c *Commands) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case dockerMCPAvailabilityCheckedMsg:
		c.dockerMCPAvailable = &msg.available
		c.dockerMCPCheckInFlight = false
		if c.selected == SystemCommands {
			// Preserve the current selection across the rebuild to avoid reset
			var prevID string
			if item, ok := c.list.SelectedItem().(*CommandItem); ok && item != nil {
				prevID = item.id
			}
			c.setCommandItems(c.selected)
			if prevID != "" {
				for i, it := range c.list.FilteredItems() {
					if ci, ok := it.(*CommandItem); ok && ci != nil && ci.id == prevID {
						c.list.SetSelected(i)
						c.list.ScrollToSelected()
						break
					}
				}
			}
		}
		return nil
	case spinner.TickMsg:
		if c.loading {
			var cmd tea.Cmd
			c.spinner, cmd = c.spinner.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, c.keyMap.Previous):
			c.list.Focus()
			if c.list.IsSelectedFirst() {
				c.list.SelectLast()
			} else {
				c.list.SelectPrev()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Next):
			c.list.Focus()
			if c.list.IsSelectedLast() {
				c.list.SelectFirst()
			} else {
				c.list.SelectNext()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Select):
			if selectedItem := c.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*CommandItem); ok && item != nil {
					return item.Action()
				}
			}
		case key.Matches(msg, c.keyMap.Tab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.nextCommandType()
				c.setCommandItems(c.selected)
			}
		case key.Matches(msg, c.keyMap.ShiftTab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.previousCommandType()
				c.setCommandItems(c.selected)
			}
		default:
			var cmd tea.Cmd
			for _, item := range c.list.FilteredItems() {
				if item, ok := item.(*CommandItem); ok && item != nil {
					if msg.String() == item.Shortcut() {
						return item.Action()
					}
				}
			}
			prevValue := c.input.Value()
			c.input, cmd = c.input.Update(msg)
			value := c.input.Value()
			if value != prevValue {
				c.list.SetFilter(value)
				c.list.ScrollToTop()
				c.list.SetSelected(0)
			}
			return ActionCmd{cmd}
		}
	}
	return nil
}

func checkDockerMCPAvailabilityCmd() tea.Cmd {
	return func() tea.Msg {
		return dockerMCPAvailabilityCheckedMsg{available: config.RefreshDockerMCPAvailability()}
	}
}

func (c *Commands) InitialCmd() tea.Cmd {
	if c.dockerMCPAvailable != nil || c.dockerMCPCheckInFlight {
		return nil
	}
	c.dockerMCPCheckInFlight = true
	return checkDockerMCPAvailabilityCmd()
}

// Cursor returns the cursor position relative to the dialog.
func (c *Commands) Cursor() *tea.Cursor {
	return InputCursor(c.com.Styles, c.input.Cursor())
}

// commandsRadioView generates the command type selector radio buttons.
func commandsRadioView(sty *styles.Styles, selected CommandType, hasUserCmds bool, hasMCPPrompts bool) string {
	if !hasUserCmds && !hasMCPPrompts {
		return ""
	}

	selectedFn := func(t CommandType) string {
		if t == selected {
			return sty.Radio.On.Padding(0, 1).Render() + sty.Radio.Label.Render(t.String())
		}
		return sty.Radio.Off.Padding(0, 1).Render() + sty.Radio.Label.Render(t.String())
	}

	parts := []string{
		selectedFn(SystemCommands),
	}

	if hasUserCmds {
		parts = append(parts, selectedFn(UserCommands))
	}
	if hasMCPPrompts {
		parts = append(parts, selectedFn(MCPPrompts))
	}

	return strings.Join(parts, " ")
}

// Draw implements [Dialog].
func (c *Commands) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	if area.Dx() != c.windowWidth && c.selected == SystemCommands {
		c.windowWidth = area.Dx()
		// since some items in the list depend on width (e.g. toggle sidebar command),
		// we need to reset the command items when width changes
		c.setCommandItems(c.selected)
	}

	innerWidth := width - c.com.Styles.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	c.input.SetWidth(dialogInputTextWidth(t, c.input, innerWidth))

	c.list.SetSize(innerWidth, max(0, height-heightOffset))

	// Hide the shortcut hints uniformly when the widest would crowd names.
	applyInfoColumnVisibility(c.list.FilteredItems(), innerWidth, commandInfoMaxPercent)

	rc := NewRenderContext(t, width)
	rc.Title = "Commands"
	rc.TitleInfo = commandsRadioView(t, c.selected, len(c.customCommands) > 0, len(c.mcpPrompts) > 0)
	inputView := t.Dialog.InputPrompt.Render(c.input.View())
	rc.AddPart(inputView)
	listView := t.Dialog.List.Height(c.list.Height()).Render(c.list.Render())
	rc.AddPart(listView)
	rc.Help = renderDialogHelp(t, &c.help, c, innerWidth)

	if c.loading {
		rc.Help = t.Dialog.HelpView.Width(innerWidth).Render(c.spinner.View() + " Generating Prompt...")
	}

	view := rc.Render()

	cur := c.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (c *Commands) ShortHelp() []key.Binding {
	return []key.Binding{
		c.keyMap.Tab,
		c.keyMap.UpDown,
		c.keyMap.Select,
		c.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (c *Commands) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{c.keyMap.Select, c.keyMap.Next, c.keyMap.Previous, c.keyMap.Tab},
		{c.keyMap.Close},
	}
}

// nextCommandType returns the next command type in the cycle.
func (c *Commands) nextCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case UserCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case MCPPrompts:
		return SystemCommands
	default:
		return SystemCommands
	}
}

// previousCommandType returns the previous command type in the cycle.
func (c *Commands) previousCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	case UserCommands:
		return SystemCommands
	case MCPPrompts:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	default:
		return SystemCommands
	}
}

// setCommandItems sets the command items based on the specified command type.
func (c *Commands) setCommandItems(commandType CommandType) {
	c.selected = commandType

	commandItems := []list.FilterableItem{}
	switch c.selected {
	case SystemCommands:
		for _, cmd := range c.defaultCommands() {
			commandItems = append(commandItems, cmd)
		}
	case UserCommands:
		for _, cmd := range c.customCommandItems() {
			commandItems = append(commandItems, cmd)
		}
	case MCPPrompts:
		for _, cmd := range c.mcpCommandItems() {
			commandItems = append(commandItems, cmd)
		}
	}

	c.list.SetItems(commandItems...)
	c.list.SetFilter("")
	c.list.ScrollToTop()
	c.list.SetSelected(0)
	c.input.SetValue("")
}

// customCommandItems converts the workspace's custom (user-authored)
// commands into CommandItems, same construction setCommandItems' user
// tab uses -- factored out so AllItems can offer them too.
func (c *Commands) customCommandItems() []*CommandItem {
	items := make([]*CommandItem, 0, len(c.customCommands))
	for _, cmd := range c.customCommands {
		var action Action
		if cmd.Skill != nil {
			action = ActionAttachSkill{ID: cmd.Skill.SkillFilePath, Name: cmd.Skill.Name}
		} else {
			action = ActionRunCustomCommand{
				Content:   cmd.Content,
				Arguments: cmd.Arguments,
				Skill:     cmd.Skill,
			}
		}
		item := NewCommandItem(c.com.Styles, "custom_"+cmd.ID, cmd.Name, "", action)
		if cmd.Skill != nil {
			item = item.WithDescription(cmd.Skill.Description)
		}
		items = append(items, item)
	}
	return items
}

// mcpCommandItems converts the connected MCP servers' prompts into
// CommandItems, same construction setCommandItems' MCP tab uses --
// factored out so AllItems can offer them too.
func (c *Commands) mcpCommandItems() []*CommandItem {
	items := make([]*CommandItem, 0, len(c.mcpPrompts))
	for _, cmd := range c.mcpPrompts {
		action := ActionRunMCPPrompt{
			Title:       cmd.Title,
			Description: cmd.Description,
			PromptID:    cmd.PromptID,
			ClientID:    cmd.ClientID,
			Arguments:   cmd.Arguments,
		}
		items = append(items, NewCommandItem(c.com.Styles, "mcp_"+cmd.ID, cmd.PromptID, "", action))
	}
	return items
}

// AllItems returns every command this dialog can offer, across all
// three tabs (system, custom, MCP prompt) as one flat list. Used by the
// "/" inline completion popup, which searches across all of them at
// once rather than making the user switch tabs first.
func (c *Commands) AllItems() []*CommandItem {
	items := c.defaultCommands()
	items = append(items, c.customCommandItems()...)
	items = append(items, c.mcpCommandItems()...)
	return items
}

// defaultCommands returns the list of default system commands.
func (c *Commands) defaultCommands() []*CommandItem {
	commands := []*CommandItem{
		NewCommandItem(c.com.Styles, "new_session", "New Session", "ctrl+n", ActionNewSession{}).WithAliases("clear").WithSlash("/new").WithSummary("Start a new chat, clearing this one"),
		NewCommandItem(c.com.Styles, "switch_session", "Sessions", "ctrl+s", ActionOpenDialog{SessionsID}).WithSlash("/sessions").WithSummary("Browse and switch between sessions"),
		NewCommandItem(c.com.Styles, "switch_model", "Switch Model", "ctrl+l", ActionOpenDialog{ModelsID}).WithSlash("/model").WithSummary("Choose the model this session runs on"),
	}

	// Leaving a sub-agent's session you stepped into. The key for this
	// (f3) only shows in the expanded help, so without a palette entry
	// the way back is easy to miss once you are already in there.
	if c.hasPreviousSession {
		commands = append(commands, NewCommandItem(c.com.Styles, "back_to_session", "Back to Previous Session", "f3", ActionBackToPreviousSession{}).WithAliases("leave", "exit agent").WithSlash("/back").WithSummary("Return to the session you stepped in from"))
	}

	// Only show compact command if there's an active session
	if c.hasSession {
		commands = append(commands, NewCommandItem(c.com.Styles, "summarize", "Summarize Session", "", ActionSummarize{SessionID: c.sessionID}).WithAliases("compact").WithSlash("/compact").WithSummary("Summarize the session to free up context"))
		commands = append(commands, NewCommandItem(c.com.Styles, "fresh", "Refresh Session (recover from a stuck run)", "", ActionFreshSession{SessionID: c.sessionID}).WithSlash("/fresh").WithSummary("Recover a session stuck mid-run"))
		commands = append(commands, NewCommandItem(c.com.Styles, "interrupt_correct", "Interrupt + Correct", "f10", ActionInterruptWithCorrection{}).WithSlash("/correct").WithSummary("Interrupt the run and correct the prompt"))
	}

	{
		autoCompactLabel := "Disable Auto-Compact"
		if cfg := c.com.Config(); cfg != nil && cfg.Options != nil && cfg.Options.DisableAutoSummarize {
			autoCompactLabel = "Enable Auto-Compact"
		}
		commands = append(commands, NewCommandItem(c.com.Styles, "toggle_auto_compact", autoCompactLabel, "", ActionToggleAutoCompact{}).WithSlash("/auto-compact").WithSummary("Turn automatic summarizing on or off"))
		commands = append(commands, NewCommandItem(c.com.Styles, "auto_compact_threshold", "Set Auto-Compact Threshold", "", ActionOpenAutoCompactThresholdForm{}).WithSlash("/auto-compact-at").WithSummary("Set how full context gets before summarizing"))
	}

	// Add reasoning toggle for models that support it
	cfg := c.com.Config()
	if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
		providerCfg := cfg.GetProviderForModel(agentCfg.Model)
		model := cfg.GetModelByType(agentCfg.Model)
		if providerCfg != nil && model != nil && model.CanReason {
			selectedModel := cfg.Models[agentCfg.Model]

			// Anthropic models: thinking toggle
			if model.CanReason && len(model.ReasoningLevels) == 0 {
				status := "Enable"
				if selectedModel.Think {
					status = "Disable"
				}
				commands = append(commands, NewCommandItem(c.com.Styles, "toggle_thinking", status+" Thinking Mode", "", ActionToggleThinking{}).WithSlash("/thinking").WithSummary("Turn extended thinking on or off"))
			}

			// OpenAI models: reasoning effort dialog
			if len(model.ReasoningLevels) > 0 {
				commands = append(commands, NewCommandItem(c.com.Styles, "select_reasoning_effort", "Select Reasoning Effort", "", ActionOpenDialog{
					DialogID: ReasoningID,
				}).WithSlash("/effort").WithSummary("Set how hard the model reasons"))
			}
		}
	}
	// Only show toggle compact mode command if window width is larger than compact breakpoint (120)
	if c.windowWidth >= sidebarCompactModeBreakpoint && c.hasSession {
		commands = append(commands, NewCommandItem(c.com.Styles, "toggle_sidebar", "Toggle Sidebar", "", ActionToggleCompactMode{}).WithSlash("/sidebar").WithSummary("Show or hide the sidebar"))
	}
	if c.hasSession {
		cfgPrime := c.com.Config()
		agentCfg := cfgPrime.Agents[config.AgentCoder]
		model := cfgPrime.GetModelByType(agentCfg.Model)
		if model != nil && model.SupportsImages {
			commands = append(commands, NewCommandItem(c.com.Styles, "file_picker", "Open File Picker", "ctrl+f", ActionOpenDialog{
				DialogID: FilePickerID,
			}).WithSlash("/attach").WithSummary("Attach an image or file to the prompt"))
		}
	}

	// Add external editor command if $EDITOR is available.
	//
	// TODO: Use [tea.EnvMsg] to get environment variable instead of os.Getenv;
	// because os.Getenv does IO is breaks the TEA paradigm and is generally an
	// antipattern.
	if os.Getenv("EDITOR") != "" {
		commands = append(commands, NewCommandItem(c.com.Styles, "open_external_editor", "Open External Editor", "ctrl+o", ActionExternalEditor{}).WithSlash("/editor").WithSummary("Write the prompt in $EDITOR"))
	}

	// Add Docker MCP command if available and not already enabled.
	if !cfg.IsDockerMCPEnabled() && c.dockerMCPAvailable != nil && *c.dockerMCPAvailable {
		commands = append(commands, NewCommandItem(c.com.Styles, "enable_docker_mcp", "Enable Docker MCP Catalog", "", ActionEnableDockerMCP{}).WithSlash("/docker-mcp").WithSummary("Enable the Docker MCP catalog"))
	}

	// Add disable Docker MCP command if it's currently enabled
	if cfg.IsDockerMCPEnabled() {
		commands = append(commands, NewCommandItem(c.com.Styles, "disable_docker_mcp", "Disable Docker MCP Catalog", "", ActionDisableDockerMCP{}).WithSlash("/docker-mcp").WithSummary("Disable the Docker MCP catalog"))
	}

	if c.hasTodos || c.hasQueue {
		var label string
		switch {
		case c.hasTodos && c.hasQueue:
			label = "Toggle To-Dos/Queue"
		case c.hasQueue:
			label = "Toggle Queue"
		default:
			label = "Toggle To-Dos"
		}
		commands = append(commands, NewCommandItem(c.com.Styles, "toggle_pills", label, "ctrl+t", ActionTogglePills{}).WithSlash("/pills").WithSummary("Show or hide the to-do and queue strip"))
	}

	// Add a command for selecting notification style via picker dialog.
	notificationLabel := "Notification Style"
	commands = append(commands, NewCommandItem(c.com.Styles, "select_notifications", notificationLabel, "", ActionOpenDialog{DialogID: NotificationsID}).WithSlash("/notifications").WithSummary("Choose how you are notified when a run ends"))

	commands = append(
		commands,
		NewCommandItem(c.com.Styles, "toggle_yolo", "Toggle Yolo Mode", "ctrl+y", ActionToggleYoloMode{}).WithSlash("/yolo").WithSummary("Run every tool without asking first"),
		NewCommandItem(c.com.Styles, "cycle_permission_mode", "Cycle Permission Mode", "ctrl+shift+y", ActionCyclePermissionMode{}).WithSlash("/permissions").WithSummary("Cycle what Atlas is allowed to do unasked"),
		NewCommandItem(c.com.Styles, "rewind", "Rewind to Checkpoint", "ctrl+shift+r", ActionOpenDialog{DialogID: RewindID}).WithSlash("/rewind").WithSummary("Roll the session back to a checkpoint"),
		NewCommandItem(c.com.Styles, "jobs", "Background Jobs", "p", ActionOpenDialog{DialogID: JobsID}).WithSlash("/jobs").WithSummary("View and manage background jobs"),
		NewCommandItem(c.com.Styles, "agent-hub", "Agent Hub", "alt+a", ActionOpenDialog{DialogID: AgentHubID}).WithSlash("/agents").WithSummary("View and switch between running agents"),
		NewCommandItem(c.com.Styles, "session-mode", sessionModeCommandLabel(c.com.Config()), "", ActionOpenDialog{DialogID: ModesID}).WithAliases("mode").WithSlash("/mode").WithSummary("Switch the session's working mode"),
		NewCommandItem(c.com.Styles, "model-roles", "Model Roles", "", ActionOpenDialog{DialogID: ModelRolesID}).WithSlash("/roles").WithSummary("Assign models to roles like title and summary"),
		NewCommandItem(c.com.Styles, "model-fallbacks", "Model Fallbacks", "", ActionOpenDialog{DialogID: FallbacksID}).WithSlash("/fallbacks").WithSummary("Set which models to fall back to on failure"),
		NewCommandItem(c.com.Styles, "subagents", "Subagents", "", ActionOpenDialog{DialogID: SubagentsID}).WithSlash("/subagents").WithSummary("Configure the subagents available to Atlas"),
		NewCommandItem(c.com.Styles, "tool-settings", "Tool Settings", "", ActionOpenDialog{DialogID: ToolSettingsID}).WithSlash("/tools").WithSummary("Choose which tools Atlas may use"),
		NewCommandItem(c.com.Styles, "fast-mode", "Fast Mode (small model, lowest reasoning)", "", ActionSetMode{Mode: "fast"}).WithSlash("/fast").WithSummary("Small model, lowest reasoning"),
		NewCommandItem(c.com.Styles, "quality-mode", "Quality Mode (large model, highest reasoning)", "", ActionSetMode{Mode: "quality"}).WithSlash("/quality").WithSummary("Large model, highest reasoning"),
		NewCommandItem(c.com.Styles, "search", "Search Chat", "f5", ActionOpenDialog{DialogID: ChatSearchID}).WithSlash("/search").WithSummary("Search this conversation"),
		NewCommandItem(c.com.Styles, "files", "Modified Files", "f", ActionOpenDialog{DialogID: FilesID}).WithSlash("/files").WithSummary("Show files changed in this session"),
		NewCommandItem(c.com.Styles, "usage", "Usage & Cost", "f6", ActionOpenDialog{DialogID: UsageID}).WithSlash("/usage").WithSummary("Show token usage and cost"),
		NewCommandItem(c.com.Styles, "snippets", "Snippets", "f7", ActionOpenDialog{DialogID: SnippetsID}).WithSlash("/snippets").WithSummary("Insert a saved prompt snippet"),
		NewCommandItem(c.com.Styles, "history", "Search Prompt History", "f8", ActionOpenDialog{DialogID: PromptHistoryID}).WithSlash("/history").WithSummary("Search prompts you have sent before"),
		NewCommandItem(c.com.Styles, "search-sessions", "Search All Sessions", "f9", ActionOpenDialog{DialogID: SessionSearchID}).WithSlash("/find").WithSummary("Search across all your sessions"),
		NewCommandItem(c.com.Styles, "toggle_help", "Toggle Help", "ctrl+g", ActionToggleHelp{}).WithSlash("/help").WithSummary("Show or hide the keyboard help"),
		NewCommandItem(c.com.Styles, "init", "Initialize Project", "", ActionInitializeProject{}).WithSlash("/init").WithSummary("Create an AGENTS.md for this project"),
	)

	// Add transparent background toggle.
	transparentLabel := "Disable Background Color"
	if cfg != nil && cfg.Options != nil && cfg.Options.TUI.IsTransparent() {
		transparentLabel = "Enable Background Color"
	}
	commands = append(commands, NewCommandItem(c.com.Styles, "toggle_transparent", transparentLabel, "", ActionToggleTransparentBackground{}).WithSlash("/transparent").WithSummary("Use the terminal's own background"))

	commands = append(
		commands,
		NewCommandItem(c.com.Styles, "quit", "Quit", "ctrl+c", tea.QuitMsg{}).WithAliases("exit").WithSlash("/quit").WithSummary("Exit Atlas"),
	)

	return commands
}

// SetCustomCommands sets the custom commands and refreshes the view if user commands are currently displayed.
func (c *Commands) SetCustomCommands(customCommands []commands.CustomCommand) {
	c.customCommands = customCommands
	if c.selected == UserCommands {
		c.setCommandItems(c.selected)
	}
}

// SetMCPPrompts sets the MCP prompts and refreshes the view if MCP prompts are currently displayed.
func (c *Commands) SetMCPPrompts(mcpPrompts []commands.MCPPrompt) {
	c.mcpPrompts = mcpPrompts
	if c.selected == MCPPrompts {
		c.setCommandItems(c.selected)
	}
}

// StartLoading implements [LoadingDialog].
func (c *Commands) StartLoading() tea.Cmd {
	if c.loading {
		return nil
	}
	c.loading = true
	return c.spinner.Tick
}

// StopLoading implements [LoadingDialog].
func (c *Commands) StopLoading() {
	c.loading = false
}
