package dialog

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/commands"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/oauth"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/skills"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/util"
)

// ActionClose is a message to close the current dialog.
type ActionClose struct{}

// ActionQuit is a message to quit the application.
type ActionQuit = tea.QuitMsg

// ActionOpenDialog is a message to open a dialog.
type ActionOpenDialog struct {
	DialogID string
}

// ActionSelectSession is a message indicating a session has been selected.
type ActionSelectSession struct {
	Session session.Session
}

// ActionViewSession is a message indicating the user wants to switch into
// viewing another session (e.g. a sub-agent's run) from the jobs dialog,
// without losing track of the session they were on before.
type ActionViewSession struct {
	SessionID string
}

// ActionOpenFileDiff is emitted from the Files dialog when the user picks a
// modified file to view its cumulative session diff.
type ActionOpenFileDiff struct {
	Entry FileDiffEntry
}

// ActionRewindApplied is a message indicating a rewind was applied
// successfully: a new forked session was created and the working
// directory's files were restored to their state as of the chosen
// checkpoint message.
type ActionRewindApplied struct {
	Result rewind.Result
}

// ActionSelectModel is a message indicating a model has been selected.
type ActionSelectModel struct {
	Provider       catwalk.Provider
	Model          config.SelectedModel
	ModelType      config.SelectedModelType
	ReAuthenticate bool
}

// Messages for commands
type (
	ActionNewSession              struct{}
	ActionToggleHelp              struct{}
	ActionToggleCompactMode       struct{}
	ActionToggleThinking          struct{}
	ActionTogglePills             struct{}
	ActionExternalEditor          struct{}
	ActionToggleYoloMode          struct{}
	ActionCyclePermissionMode     struct{}
	ActionToggleNotifications     struct{}
	ActionSelectNotificationStyle struct {
		Style string
	}
	ActionToggleTransparentBackground struct{}
	ActionToggleAutoCompact           struct{}
	ActionInitializeProject           struct{}
	ActionSummarize                   struct {
		SessionID string
	}
	// ActionShowGoal reports what the session is working towards.
	// Setting a goal is done inline -- "/goal <what to reach>" -- so
	// there is nothing here to carry an answer back from.
	ActionShowGoal struct {
		SessionID string
	}
	// ActionFreshSession is a message indicating the user wants to
	// recover a session that looks stuck or stale: cancel whatever is
	// running, then reload the session's messages from the backend so
	// the chat view resyncs with the source of truth. See /fresh in
	// commands.go.
	ActionFreshSession struct {
		SessionID string
	}
	// ActionInterruptWithCorrection is a message indicating the user
	// wants to stop the current turn and correct course from where it
	// left off, rather than waiting for it to finish or losing the
	// partial response entirely. See interruptWithCorrection in ui.go.
	ActionInterruptWithCorrection struct{}

	// ActionOpenModelRoleForm opens the add/edit form for a model role.
	// An empty ExistingName means creating a new role, in which case
	// the form includes a name field; editing an existing one keeps its
	// name fixed and only offers provider/model/reasoning_effort.
	ActionOpenModelRoleForm struct {
		ExistingName            string
		ExistingProvider        string
		ExistingModel           string
		ExistingReasoningEffort string
	}
	// ActionSaveModelRole is the model role form's result. Args holds
	// "name" (create only), "provider", "model", "reasoning_effort"
	// (optional -- empty leaves the model's own default in effect).
	ActionSaveModelRole struct {
		ExistingName string
		Args         map[string]string
	}

	// ActionOpenFallbackEntryForm opens the add-fallback form for one
	// model type's chain.
	ActionOpenFallbackEntryForm struct {
		ModelType config.SelectedModelType
	}
	// ActionSaveFallbackEntry is the fallback-entry form's result: Args
	// holds "provider", "model", appended to ModelType's chain.
	ActionSaveFallbackEntry struct {
		ModelType config.SelectedModelType
		Args      map[string]string
	}
	// ActionOpenFallbackCooldownForm opens the cooldown-editing form.
	ActionOpenFallbackCooldownForm struct {
		Current int
	}
	// ActionSaveFallbackCooldown is the cooldown form's result: Args
	// holds "seconds".
	ActionSaveFallbackCooldown struct {
		Args map[string]string
	}

	// ActionBackToPreviousSession returns to the session the user came
	// from after stepping into another one (a sub-agent's run, say).
	ActionBackToPreviousSession struct{}

	// ActionSelectSessionMode puts the session into a mode, or -- with
	// an empty Mode -- back onto the ordinary coder prompt.
	ActionSelectSessionMode struct {
		Mode string
	}

	// ActionOpenAutoCompactThresholdForm opens the auto-compact
	// threshold-editing form.
	ActionOpenAutoCompactThresholdForm struct{}
	// ActionSaveAutoCompactThreshold is the threshold form's result:
	// Args holds "percent" -- empty resets to the built-in thresholds.
	ActionSaveAutoCompactThreshold struct {
		Args map[string]string
	}

	// ActionOpenSubagentForm opens the add/edit form for a subagent's
	// name, description, and model role. An empty ExistingName means
	// creating a new one, in which case the form includes a name field
	// and UserScope decides which directory it is written to; editing
	// an existing one keeps its name and directory fixed.
	ActionOpenSubagentForm struct {
		ExistingName        string
		ExistingDescription string
		ExistingModel       string
		UserScope           bool
	}
	// ActionSaveSubagentMeta is the subagent form's result. Args holds
	// "name" (create only), "description", "model".
	ActionSaveSubagentMeta struct {
		ExistingName string
		UserScope    bool
		Args         map[string]string
	}
	// ActionEditSubagentFile opens a subagent's definition file in
	// $EDITOR, for the instructions body the form does not edit.
	ActionEditSubagentFile struct {
		Name string
		Path string
	}

	// ActionSetMode switches the coder agent between the small model
	// ("fast": quicker, cheaper, lowest available reasoning effort) and
	// the large model ("quality": the model actually chosen as the
	// session's primary, highest available reasoning effort). See
	// handleSetMode in ui.go.
	ActionSetMode struct {
		Mode string // "fast" or "quality"
	}
	// ActionSelectReasoningEffort is a message indicating a reasoning effort
	// has been selected.
	ActionSelectReasoningEffort struct {
		Effort string
	}
	ActionPermissionResponse struct {
		Permission permission.PermissionRequest
		Action     PermissionAction
	}
	// ActionRunCustomCommand is a message to run a custom command.
	ActionRunCustomCommand struct {
		Content   string
		Arguments []commands.Argument
		Args      map[string]string // Actual argument values
		Skill     *skills.Skill     // Set when this is a skill command
	}
	// ActionAttachSkill is sent when a skill is selected from the commands
	// dialog to be attached to the conversation as a markdown attachment.
	ActionAttachSkill struct {
		ID   string
		Name string
	}
	// ActionRunMCPPrompt is a message to run a custom command.
	ActionRunMCPPrompt struct {
		Title       string
		Description string
		PromptID    string
		ClientID    string
		Arguments   []commands.Argument
		Args        map[string]string // Actual argument values
	}
	// ActionEnableDockerMCP is a message to enable Docker MCP.
	ActionEnableDockerMCP struct{}
	// ActionDisableDockerMCP is a message to disable Docker MCP.
	ActionDisableDockerMCP struct{}
)

// Messages for MCP OAuth authentication dialog.
type (
	// ActionMCPAuthStarted is sent when the user approves authentication
	// for an MCP server. The UI should initiate the actual auth flow
	// using the provided context, which the dialog will cancel if the
	// user closes it.
	ActionMCPAuthStarted struct {
		Name string
		Ctx  context.Context
	}

	// ActionMCPAuthComplete is sent when MCP authentication succeeds.
	ActionMCPAuthComplete struct {
		Name string
	}

	// ActionMCPAuthErrored is sent when MCP authentication fails.
	ActionMCPAuthErrored struct {
		Name  string
		Error error
	}
)

// Messages for API key input dialog.
type (
	ActionChangeAPIKeyState struct {
		State APIKeyInputState
	}
)

// Messages for OAuth2 device flow dialog.
type (
	// ActionInitiateOAuth is sent when the device auth is initiated
	// successfully.
	ActionInitiateOAuth struct {
		DeviceCode      string
		UserCode        string
		ExpiresIn       int
		VerificationURL string
		Interval        int
	}

	// ActionCompleteOAuth is sent when the device flow completes successfully.
	ActionCompleteOAuth struct {
		Token *oauth.Token
	}

	// ActionOAuthErrored is sent when the device flow encounters an error.
	ActionOAuthErrored struct {
		Error error
	}
)

// ActionCmd represents an action that carries a [tea.Cmd] to be passed to the
// Bubble Tea program loop.
type ActionCmd struct {
	Cmd tea.Cmd
}

// ActionFilePickerSelected is a message indicating a file has been selected in
// the file picker dialog.
type ActionFilePickerSelected struct {
	Path string
}

// Cmd returns a command that reads the file at path and sends a
// [message.Attachement] to the program.
func (a ActionFilePickerSelected) Cmd() tea.Cmd {
	path := a.Path
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		isFileLarge, err := common.IsFileTooBig(path, common.MaxAttachmentSize)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}
		if isFileLarge {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "file too large, max 5MB",
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)

		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}
