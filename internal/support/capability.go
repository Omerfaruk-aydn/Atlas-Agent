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

