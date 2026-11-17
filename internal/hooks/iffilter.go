package hooks

import (
	"encoding/json"
	"strings"
)

// matchesIf reports whether a hook's "if" condition holds for a tool call.
// The condition is Tool(pattern) or just Tool: the tool name must equal
// Tool (case-insensitively) and, when a pattern is given, the call's main
// argument (command, file_path, path, url or pattern, whichever the call
// has first) must match it, where * stands for any run of characters.
// An empty condition always holds. A condition on an event with no tool
// (such as UserPromptSubmit) does not hold.
func matchesIf(condition, toolName, toolInputJSON string) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true
	}
	name, pattern, hasPattern := strings.Cut(condition, "(")
	name = strings.TrimSpace(name)
	if name != "*" && !strings.EqualFold(name, toolName) {
		return false
	}
	if !hasPattern {
		return true
	}
	pattern = strings.TrimSuffix(strings.TrimSpace(pattern), ")")
	return wildcard(pattern, mainArgument(toolInputJSON))
}

func mainArgument(toolInputJSON string) string {
	var in map[string]any
	if json.Unmarshal([]byte(toolInputJSON), &in) != nil {
		return ""
	}
	for _, key := range []string{"command", "file_path", "path", "url", "pattern"} {
		if s, ok := in[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// wildcard matches s against pattern, where * matches any run of
// characters (including none) and everything else matches literally.
func wildcard(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
