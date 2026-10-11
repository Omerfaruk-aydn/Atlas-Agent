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

// IsFileCapability reports permissions that need a filesystem access scope.
func IsFileCapability(c Capability) bool {
	return c == CapFileRead || c == CapFileSend || c == CapFileReceive || c == CapFileModify
}

// HighImpact reports capabilities the owner should grant deliberately.
func (c Capability) HighImpact() bool {
	switch c {
	case CapFileModify, CapCommand, CapInput, CapInstall, CapServices, CapRestart:
		return true
	}
	return false
}

// Presets are starting scopes the owner can adjust before creating a code.
var Presets = map[string][]Capability{
	"diagnose": {CapSystemInfo, CapFileRead, CapScreenView},
	"assist":   {CapSystemInfo, CapFileRead, CapFileSend, CapFileReceive, CapScreenView, CapInput, CapBrowser},
	"full":     {CapSystemInfo, CapFileRead, CapFileSend, CapFileReceive, CapFileModify, CapCommand, CapScreenView, CapInput, CapBrowser, CapInstall, CapServices, CapRestart},
}

// ParseCapabilities validates a comma-separated list or a preset name.
func ParseCapabilities(s string) ([]Capability, error) {
	if preset, ok := Presets[strings.TrimSpace(s)]; ok {
		return slices.Clone(preset), nil
	}
	var out []Capability
	for part := range strings.SplitSeq(s, ",") {
		c := Capability(strings.TrimSpace(part))
		if c == "" {
			continue
		}
		if !slices.Contains(AllCapabilities, c) {
			return nil, fmt.Errorf("unknown capability %q", c)
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return Normalize(out), nil
}

// Normalize deduplicates and orders capabilities.
func Normalize(caps []Capability) []Capability {
	var out []Capability
	for _, c := range AllCapabilities {
		if slices.Contains(caps, c) {
			out = append(out, c)
		}
	}
	return out
}

// Strings converts for wire and display.
func Strings(caps []Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

// requiredFor maps a node API need onto the capabilities that satisfy it.
// Any one listed capability is enough.
func requiredFor(need nodes.Need) []Capability {
	switch need {
	case nodes.NeedInfo, nodes.NeedOps:
		return nil
	case nodes.NeedSystemInfo:
		return []Capability{CapSystemInfo}
	case nodes.NeedExec:
		return []Capability{CapCommand}
	case nodes.NeedFileList:
		return []Capability{CapFileRead, CapFileSend}
	case nodes.NeedFileRead:
		return []Capability{CapFileSend}
	case nodes.NeedFileWrite:
		return []Capability{CapFileReceive}
	case nodes.NeedFileModify:
		return []Capability{CapFileModify}
	case nodes.NeedDesktopView:
		return []Capability{CapScreenView}
	case nodes.NeedDesktopInput:
		return []Capability{CapInput}
	case nodes.NeedDesktopLaunch:
		return []Capability{CapInput, CapBrowser}
	case nodes.NeedBrowser, nodes.NeedBrowserInput:
		return []Capability{CapBrowser}
	}
	return []Capability{"__never__"}
}

// mutating reports needs that change the remote computer. A paused agent
// task refuses them; reads and status stay available.
func mutating(need nodes.Need) bool {
	switch need {
	case nodes.NeedExec, nodes.NeedFileWrite, nodes.NeedFileModify, nodes.NeedDesktopInput, nodes.NeedDesktopLaunch, nodes.NeedBrowserInput:
		return true
	}
	return false
}

// installerPrograms are package managers and installers; running them
// needs the install capability on top of command.
var installerPrograms = []string{"winget", "msiexec", "choco", "scoop", "apt", "apt-get", "dnf", "yum", "zypper", "pacman", "brew", "snap", "flatpak", "installer", "pkg"}

var servicePrograms = []string{"sc", "net", "netsh", "systemctl", "service", "launchctl", "reg", "bcdedit", "schtasks", "gpupdate", "sfc", "dism"}

var restartPrograms = []string{"shutdown", "reboot", "poweroff", "halt", "restart-computer", "stop-computer"}

var browserPrograms = []string{"msedge", "chrome", "firefox", "brave", "opera", "vivaldi", "iexplore", "safari", "chromium", "chromium-browser", "google-chrome"}

var shellPrograms = []string{"powershell", "pwsh", "cmd", "bash", "sh", "zsh"}

// programName is the lowercase base name of argv[0] without extension.
func programName(arg string) string {
	arg = strings.ToLower(strings.ReplaceAll(arg, "\\", "/"))
	if i := strings.LastIndex(arg, "/"); i >= 0 {
		arg = arg[i+1:]
	}
	for _, ext := range []string{".exe", ".com", ".bat", ".cmd", ".ps1", ".msi"} {
		arg = strings.TrimSuffix(arg, ext)
	}
	return arg
}

