package browser

import (
	"fmt"
	"strings"
)

// keyAliases maps common spellings to the names resolveInputKey accepts.
var keyAliases = map[string]string{
	"return": "enter", "esc": "escape", "del": "delete", "bksp": "backspace",
	"up": "arrowup", "down": "arrowdown", "left": "arrowleft", "right": "arrowright",
	"spacebar": "space", " ": "space", "pgup": "pageup", "pgdn": "pagedown",
	"page_up": "pageup", "page_down": "pagedown", "arrow_up": "arrowup", "arrow_down": "arrowdown",
	"arrow_left": "arrowleft", "arrow_right": "arrowright", "minus": "-", "equal": "=",
	"comma": ",", "period": ".", "slash": "/", "backslash": "\\", "semicolon": ";", "quote": "'",
}

// ErrUnsupportedKey names a key the keyboard path cannot send. It is the
// same error for key, key_combo and workflow steps so one recovery applies.
type ErrUnsupportedKey struct{ Key string }

func (e ErrUnsupportedKey) Error() string {
	return fmt.Sprintf("unsupported_key: %q is not a sendable key. Use one letter (a-z), digit, punctuation (- = [ ] / \\ , . ; ' `), enter, tab, escape, backspace, delete, space, home, end, pageup, pagedown, arrowup/down/left/right or f1-f12, with optional modifiers alt, ctrl, meta, shift (\"ctrl+a\" or key_combo modifiers). Nothing was sent", e.Key)
}

// NormalizeKey parses a key or chord such as "f", "Enter", "ctrl+a",
// "Shift+1" or "cmd+shift+z" into the key and modifiers key_combo sends.
// Modifier names are validated by parseModifiers; the key by
// resolveInputKey, so layout and cleanup rules stay in one place.
func NormalizeKey(raw string) (string, []string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", nil, ErrUnsupportedKey{Key: raw}
	}
	parts := []string{text}
	if len(text) > 1 && strings.Contains(text, "+") {
		parts = strings.Split(text, "+")
		if strings.HasSuffix(text, "++") {
			// "ctrl++" names the plus key itself, which shares "=".
			parts = append(strings.Split(strings.TrimSuffix(text, "++"), "+"), "=")
		}
	}
	key := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if alias, ok := keyAliases[key]; ok {
		key = alias
	}
	var modifiers []string
	for _, m := range parts[:len(parts)-1] {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			return "", nil, ErrUnsupportedKey{Key: raw}
		}
		if _, ok := modifierBits[m]; !ok {
			return "", nil, ErrUnsupportedKey{Key: raw}
		}
		modifiers = append(modifiers, m)
	}
	mask, _ := parseModifiers(modifiers)
	if _, ok := resolveInputKey(key, mask&8 != 0); !ok {
		return "", nil, ErrUnsupportedKey{Key: raw}
	}
	return key, modifiers, nil
}
