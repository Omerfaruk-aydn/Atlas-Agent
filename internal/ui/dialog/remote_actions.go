package dialog

import (
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/mobile"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/remotecontrol"
)

// RemoteID identifies the remote control dialog (/rc).
const RemoteID = "remote"

// ActionOpenRemote opens the remote control dialog.
type ActionOpenRemote struct{}

// ActionRemoteSaveProfile asks the UI to validate and save a profile. The
// draft may carry a secret; the UI seals it and never stores it in config.
type ActionRemoteSaveProfile struct{ Draft remotecontrol.Draft }

// ActionRemoteSetActive makes a profile the active one.
type ActionRemoteSetActive struct{ ID string }

// ActionRemoteDeleteProfile forgets a profile on this computer only.
type ActionRemoteDeleteProfile struct{ ID string }

// ActionRemoteEnable turns remote access on for a profile: the relay
// settings are written and the phone gateway restarts with the relay.
type ActionRemoteEnable struct{ ID string }

// ActionRemoteDisable turns remote access off on this computer. The server
// keeps running; removing it is a separate action.
type ActionRemoteDisable struct{}

// ActionRemotePair opens phone pairing and device management.
type ActionRemotePair struct{}

// ActionRemoteLAN discovers LAN addresses, or starts the chosen address.
// It never changes the saved relay or mobile configuration.
type ActionRemoteLAN struct{ Address string }

// ActionRemoteBrowserHelp asks the agent to help in the provider's web
// panel with the browser tool.
type ActionRemoteBrowserHelp struct{}

// ActionRemoteImportDraft fills the form from the assistant's findings.
type ActionRemoteImportDraft struct{}

// RemoteEnv is what the dialog reads from the UI. Every function is called
// on the UI goroutine.
type RemoteEnv struct {
	Manager *remotecontrol.Manager
	// Gateway returns the running phone gateway, or nil.
	Gateway func() *mobile.Gateway
	// Starting reports a gateway start in progress.
	Starting func() bool
	// StartErr is the last gateway start failure for remote access.
	StartErr func() error
	// HomeLAN reports the temporary home-network mode, including startup.
	HomeLAN func() bool
}
