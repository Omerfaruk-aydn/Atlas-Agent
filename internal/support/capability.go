package support

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
)

// Capability is one function the computer owner grants.
type Capability string

const (
	CapSystemInfo  Capability = "system_info"
	CapFileRead    Capability = "file_read"
	CapFileSend    Capability = "file_send"
	CapFileReceive Capability = "file_receive"
	CapFileModify  Capability = "file_modify"
	CapCommand     Capability = "command"
	CapScreenView  Capability = "screen_view"
	CapInput       Capability = "input"
	CapBrowser     Capability = "browser"
	CapInstall     Capability = "install"
	CapServices    Capability = "services"
	CapRestart     Capability = "restart"
)

// AllCapabilities lists every capability in display order.
var AllCapabilities = []Capability{CapSystemInfo, CapFileRead, CapFileSend, CapFileReceive, CapFileModify, CapCommand, CapScreenView, CapInput, CapBrowser, CapInstall, CapServices, CapRestart}

// Label is the English source text for a capability; the TUI translates it.
func (c Capability) Label() string {
	switch c {
	case CapSystemInfo:
		return "System information and diagnostics"
	case CapFileRead:
		return "Read files"
	case CapFileSend:
		return "Send files to the supporter"
	case CapFileReceive:
		return "Receive files from the supporter"
	case CapFileModify:
		return "Change or replace files"
	case CapCommand:
		return "Run commands"
	case CapScreenView:
		return "View the screen"
	case CapInput:
		return "Mouse and keyboard control"
	case CapBrowser:
		return "Control a browser on this computer"
	case CapInstall:
		return "Install or remove programs"
	case CapServices:
		return "Change services and system settings"
	case CapRestart:
		return "Request a restart"
	}
	return string(c)
}

