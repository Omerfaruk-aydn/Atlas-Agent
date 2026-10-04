package model

import "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-widgets/v2/key"

type KeyMap struct {
	Editor struct {
		SendMessage key.Binding
		OpenEditor  key.Binding
		Newline     key.Binding
		AddImage    key.Binding
		PasteImage  key.Binding
		MentionFile key.Binding

		// Attachments key maps
		AttachmentDeleteMode key.Binding
		Escape               key.Binding
		DeleteAllAttachments key.Binding

		// History navigation
		HistoryPrev key.Binding
		HistoryNext key.Binding

		// CopySelection copies the current textarea selection to the
		// clipboard.
		CopySelection key.Binding

		// CutSelection copies the current textarea selection to the
		// clipboard and deletes it from the textarea.
		CutSelection key.Binding

		// SelectAll selects all text in the textarea.
		SelectAll key.Binding

		// PasteText pastes clipboard text into the textarea, as an
		// alternative to bracketed paste.
		PasteText key.Binding
		Dictate   key.Binding
	}

	Chat struct {
		NewSession     key.Binding
		AddAttachment  key.Binding
		Cancel         key.Binding
		Tab            key.Binding
		Details        key.Binding
		TogglePills    key.Binding
		PillLeft       key.Binding
		PillRight      key.Binding
		Down           key.Binding
		Up             key.Binding
		UpDown         key.Binding
		DownOneItem    key.Binding
		UpOneItem      key.Binding
		UpDownOneItem  key.Binding
		PageDown       key.Binding
		PageUp         key.Binding
		HalfPageDown   key.Binding
		HalfPageUp     key.Binding
		Home           key.Binding
		End            key.Binding
		EndFollow      key.Binding
		Copy           key.Binding
		ClearHighlight key.Binding
		Expand         key.Binding
		ScrollLeft     key.Binding
		ScrollRight    key.Binding
		FocusSidebar   key.Binding
		FocusChat      key.Binding
	}

	Initialize struct {
		Yes,
		No,
		Enter,
		Switch key.Binding
	}

	// Global key maps
	Quit                    key.Binding
	Help                    key.Binding
	Commands                key.Binding
	Models                  key.Binding
	Suspend                 key.Binding
	Sessions                key.Binding
	Tab                     key.Binding
	ToggleYolo              key.Binding
	CyclePermissionMode     key.Binding
	Rewind                  key.Binding
	Jobs                    key.Binding
	AgentHub                key.Binding
	Workflow                key.Binding
	InterruptWithCorrection key.Binding
	BackToSession           key.Binding
	FocusMode               key.Binding
	ChatSearch              key.Binding
	Files                   key.Binding
	Usage                   key.Binding
	Snippets                key.Binding
	PromptHistorySearch     key.Binding
	SessionSearch           key.Binding
}

func DefaultKeyMap() KeyMap {
	km := KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "more"),
		),
		Commands: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("ctrl+p", "commands"),
		),
		Models: key.NewBinding(
			key.WithKeys("ctrl+m", "ctrl+l"),
			key.WithHelp("ctrl+l", "models"),
		),
		Suspend: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("ctrl+z", "suspend"),
		),
		Sessions: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "sessions"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "change focus"),
		),
		ToggleYolo: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("ctrl+y", "toggle yolo"),
		),
		CyclePermissionMode: key.NewBinding(
			key.WithKeys("shift+tab", "ctrl+shift+y"),
			key.WithHelp("shift+tab", "cycle permission mode"),
		),
		Rewind: key.NewBinding(
			// f2 is the reliable fallback: legacy Windows consoles
			// (conhost/cmd.exe) often can't distinguish ctrl+shift+<letter>
			// from ctrl+<letter> (the Shift bit gets lost), so
			// ctrl+shift+r silently fails to fire there. F-keys don't
			// have that ambiguity in any terminal.
			key.WithKeys("ctrl+shift+r", "f2"),
			key.WithHelp("f2", "rewind"),
		),
		Jobs: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "jobs"),
		),
		Workflow: key.NewBinding(key.WithKeys("alt+w"), key.WithHelp("alt+w", "agent controls")),
		AgentHub: key.NewBinding(
			key.WithKeys("alt+a"),
			key.WithHelp("alt+a", "agent hub"),
		),
		InterruptWithCorrection: key.NewBinding(
			// f10 fallback for the same conhost/cmd.exe ctrl+shift+<letter>
			// ambiguity noted on Rewind/BackToSession above.
			key.WithKeys("ctrl+shift+i", "f10"),
			key.WithHelp("f10", "interrupt + correct"),
		),
		BackToSession: key.NewBinding(
			// f3 fallback for the same reason as Rewind's f2 (see there).
			key.WithKeys("ctrl+shift+b", "f3"),
			key.WithHelp("f3", "back to previous session"),
		),
		FocusMode: key.NewBinding(
			key.WithKeys("f4"),
			key.WithHelp("f4", "focus mode"),
		),
		ChatSearch: key.NewBinding(
			key.WithKeys("f5"),
			key.WithHelp("f5", "search chat"),
		),
		Files: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "modified files"),
		),
		Usage: key.NewBinding(
			key.WithKeys("f6"),
			key.WithHelp("f6", "usage"),
		),
		Snippets: key.NewBinding(
			key.WithKeys("f7"),
			key.WithHelp("f7", "snippets"),
		),
		PromptHistorySearch: key.NewBinding(
			key.WithKeys("f8"),
			key.WithHelp("f8", "search prompt history"),
		),
		SessionSearch: key.NewBinding(
			key.WithKeys("f9"),
			key.WithHelp("f9", "search all sessions"),
		),
	}

	km.Editor.SendMessage = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "send"),
	)
	km.Editor.OpenEditor = key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "open editor"),
	)
	km.Editor.Newline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		// "ctrl+j" is a common keybinding for newline in many editors. If
		// the terminal supports "shift+enter", we substitute the help tex
		// to reflect that.
		key.WithHelp("ctrl+j", "newline"),
	)
	km.Editor.AddImage = key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "add file"),
	)
	km.Editor.PasteImage = key.NewBinding(
		// Terminals commonly bind ctrl+v to their own paste and consume the
		// key before an application ever sees it — Windows Terminal ships
		// that binding by default. When they do, ctrl+v pastes the
		// clipboard's text and an image on the clipboard simply never
		// arrives. ctrl+alt+v is the escape hatch: no terminal claims it, so
		// it reaches here whatever the host is configured to do.
		key.WithKeys("ctrl+v", "ctrl+alt+v"),
		key.WithHelp("ctrl+v/ctrl+alt+v", "paste files, images or text"),
	)
	km.Editor.PasteText = key.NewBinding(
		key.WithKeys("ctrl+shift+v"),
		key.WithHelp("ctrl+shift+v", "paste text"),
	)
	km.Editor.Dictate = key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "microphone dictation"))
	km.Editor.MentionFile = key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "mention file"),
	)
	km.Editor.AttachmentDeleteMode = key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	)
	km.Editor.Escape = key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "cancel delete mode"),
	)
	km.Editor.DeleteAllAttachments = key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attachments"),
	)
	km.Editor.HistoryPrev = key.NewBinding(
		key.WithKeys("up"),
	)
	km.Editor.HistoryNext = key.NewBinding(
		key.WithKeys("down"),
	)
	km.Editor.CopySelection = key.NewBinding(
		key.WithKeys("ctrl+shift+c"),
		key.WithHelp("ctrl+shift+c", "copy selection"),
	)
	km.Editor.CutSelection = key.NewBinding(
		key.WithKeys("ctrl+shift+x"),
		key.WithHelp("ctrl+shift+x", "cut selection"),
	)
	km.Editor.SelectAll = key.NewBinding(
		key.WithKeys("ctrl+shift+a"),
		key.WithHelp("ctrl+shift+a", "select all"),
	)

	km.Chat.NewSession = key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	)
	km.Chat.AddAttachment = key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "add attachment"),
	)
	km.Chat.Cancel = key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "cancel"),
	)
	km.Chat.Tab = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "change focus"),
	)
	km.Chat.Details = key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "toggle details"),
	)
	km.Chat.TogglePills = key.NewBinding(
		key.WithKeys("ctrl+t", "ctrl+space"),
		key.WithHelp("ctrl+t", "toggle tasks"),
	)
	km.Chat.PillLeft = key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←/→", "switch section"),
	)
	km.Chat.PillRight = key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("←/→", "switch section"),
	)

	km.Chat.Down = key.NewBinding(
		key.WithKeys("down", "ctrl+j", "j"),
		key.WithHelp("↓", "down"),
	)
	km.Chat.Up = key.NewBinding(
		key.WithKeys("up", "ctrl+k", "k"),
		key.WithHelp("↑", "up"),
	)
	km.Chat.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑↓", "scroll"),
	)
	km.Chat.UpOneItem = key.NewBinding(
		key.WithKeys("shift+up", "K"),
		key.WithHelp("shift+↑", "up one item"),
	)
	km.Chat.DownOneItem = key.NewBinding(
		key.WithKeys("shift+down", "J"),
		key.WithHelp("shift+↓", "down one item"),
	)
	km.Chat.UpDownOneItem = key.NewBinding(
		key.WithKeys("shift+up", "shift+down"),
		key.WithHelp("shift+↑↓", "scroll one item"),
	)
	km.Chat.HalfPageDown = key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "half page down"),
	)
	km.Chat.PageDown = key.NewBinding(
		key.WithKeys("pgdown", " ", "f"),
		key.WithHelp("f/pgdn", "page down"),
	)
	km.Chat.PageUp = key.NewBinding(
		key.WithKeys("pgup", "b"),
		key.WithHelp("b/pgup", "page up"),
	)
	km.Chat.HalfPageUp = key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "half page up"),
	)
	km.Chat.Home = key.NewBinding(
		key.WithKeys("g", "home"),
		key.WithHelp("g", "home"),
	)
	km.Chat.End = key.NewBinding(
		key.WithKeys("G", "end"),
		key.WithHelp("G", "end"),
	)
	km.Chat.EndFollow = key.NewBinding(
		key.WithKeys("ctrl+end"),
	)
	km.Chat.Copy = key.NewBinding(
		key.WithKeys("c", "y", "C", "Y"),
		key.WithHelp("c/y", "copy"),
	)
	km.Chat.ClearHighlight = key.NewBinding(
		key.WithKeys("esc", "alt+esc"),
		key.WithHelp("esc", "clear selection"),
	)
	km.Chat.Expand = key.NewBinding(
		key.WithKeys("space"),
		key.WithHelp("space", "expand/collapse"),
	)
	km.Chat.ScrollLeft = key.NewBinding(
		key.WithKeys("shift+left", "H"),
		key.WithHelp("shift+←/H", "scroll left"),
	)
	km.Chat.ScrollRight = key.NewBinding(
		key.WithKeys("shift+right", "L"),
		key.WithHelp("shift+→/L", "scroll right"),
	)
	km.Chat.FocusSidebar = key.NewBinding(
		key.WithKeys("l", "right"),
		key.WithHelp("l/→", "focus sidebar"),
	)
	km.Chat.FocusChat = key.NewBinding(
		key.WithKeys("h", "left"),
		key.WithHelp("h/←", "focus chat"),
	)
	km.Initialize.Yes = key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "yes"),
	)
	km.Initialize.No = key.NewBinding(
		key.WithKeys("n", "N", "esc", "alt+esc"),
		key.WithHelp("n", "no"),
	)
	km.Initialize.Switch = key.NewBinding(
		key.WithKeys("left", "right", "tab"),
		key.WithHelp("tab", "switch"),
	)
	km.Initialize.Enter = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	)

	return km
}
