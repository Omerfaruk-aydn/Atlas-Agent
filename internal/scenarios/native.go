package scenarios

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// NativeTarget identifies scenarios exercised through the existing device
// tools, with no fallback to a browser or the host desktop.
func NativeTarget(target string) bool { return target == "desktop" || target == "android" }

var nativeNodeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

func nativeSelector(target, selector string) bool {
	if selector == "" || len(selector) > 512 || strings.ContainsAny(selector, "\x00\r\n") {
		return false
	}
	if target == "android" {
		return strings.Contains(selector, ":id/")
	}
	return strings.HasPrefix(selector, "id:") && len(selector) > 3 || strings.HasPrefix(selector, "name:") && len(selector) > 5
}

func validateNative(s Scenario) error {
	if s.URL != "" || len(s.Argv) != 0 {
		return fmt.Errorf("native scenarios target an already opened application, without URL or argv")
	}
	if s.Target == "desktop" {
		id, err := strconv.ParseUint(s.WindowID, 10, 64)
		if err != nil || id == 0 || s.Node != "" && !nativeNodeName.MatchString(s.Node) || s.Serial != "" || s.Package != "" {
			return fmt.Errorf("desktop scenario requires a numeric window_id and an optional isolated node name")
		}
	} else if s.Serial == "" || len(s.Serial) > 256 || strings.ContainsAny(s.Serial, "\x00\r\n") || s.Package == "" || len(s.Package) > 256 || strings.ContainsAny(s.Package, " /\x00\r\n") || s.Node != "" || s.WindowID != "" {
		return fmt.Errorf("android scenario requires an explicit serial and foreground package")
	}
	for _, step := range s.Steps {
		if !nativeSelector(s.Target, step.Selector) || len(step.Value) > 4096 || step.Width != 0 || step.Height != 0 {
			return fmt.Errorf("native steps require a bounded stable selector and no viewport changes")
		}
		switch step.Action {
		case "click":
			if step.Value != "" {
				return fmt.Errorf("click does not accept a value")
			}
		case "type":
			if step.Value == "" {
				return fmt.Errorf("type requires nonempty text")
			}
		case "key":
			switch step.Value {
			case "enter", "tab", "escape", "backspace", "delete", "arrowup", "arrowdown", "arrowleft", "arrowright":
			default:
				return fmt.Errorf("unsupported native scenario key")
			}
		default:
			return fmt.Errorf("unsupported native scenario action %q", step.Action)
		}
	}
	for _, a := range s.Assertions {
		if !nativeSelector(s.Target, a.Selector) || len(a.Expected) > 4096 {
			return fmt.Errorf("native assertion requires a bounded stable selector")
		}
		switch a.Kind {
		case "text", "value":
			if a.Expected == "" {
				return fmt.Errorf("native text/value assertion needs nonempty expected text")
			}
		case "visible", "focused":
			if a.Expected != "" && a.Expected != "true" {
				return fmt.Errorf("native visibility/focus assertions require positive observation")
			}
		default:
			return fmt.Errorf("unsupported native assertion %q", a.Kind)
		}
	}
	return nil
}
