package i18n

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Message is Atlas-authored text that services produce and interfaces
// render in the reader's language. Key is a catalog source literal, used as
// a printf format when Args are present. Text that did not come from Atlas
// (an error from a server, a program's output) is wrapped with Raw so it is
// shown unchanged and never formatted or looked up.
//
// In JSON a message without arguments is a plain string; one with
// arguments is {"key": ..., "args": [...]}, so it can be localized again
// after it was stored.
type Message struct {
	Key  string
	Args []any
	raw  bool
}

// M builds a message from a catalog literal.
func M(key string, args ...any) Message { return Message{Key: key, Args: args} }

// Raw wraps text that must be shown as is.
func Raw(text string) Message { return Message{Key: text, raw: true} }

// IsZero reports whether the message is empty.
func (m Message) IsZero() bool { return m.Key == "" }

// IsRaw reports whether the message is external text.
func (m Message) IsRaw() bool { return m.raw }

// String renders English.
func (m Message) String() string { return m.Localize(nil) }

// Localize renders with a translation function such as Translator.Text. A
// nil function renders English.
func (m Message) Localize(text func(string) string) string {
	if m.Key == "" {
		return ""
	}
	if m.raw {
		return m.Key
	}
	key := m.Key
	if text != nil {
		key = text(key)
	}
	if len(m.Args) == 0 {
		return key
	}
	args := make([]any, len(m.Args))
	for i, a := range m.Args {
		switch v := a.(type) {
		case float64:
			// JSON round trips turn integers into float64; keep %d working.
			if v == float64(int64(v)) {
				a = int64(v)
			}
		case Message:
			a = v.Localize(text)
		}
		args[i] = a
	}
	return fmt.Sprintf(key, args...)
}

// Map applies fn to the raw key and string arguments, e.g. to redact.
func (m Message) Map(fn func(string) string) Message {
	out := Message{Key: m.Key, raw: m.raw}
	if m.raw {
		out.Key = fn(m.Key)
	}
	if len(m.Args) > 0 {
		out.Args = make([]any, len(m.Args))
		for i, a := range m.Args {
			switch v := a.(type) {
			case string:
				a = fn(v)
			case Message:
				a = v.Map(fn)
			}
			out.Args[i] = a
		}
	}
	return out
}

// Join renders a and b as two sentences; either may be empty.
func Join(a, b Message) Message {
	switch {
	case a.IsZero():
		return b
	case b.IsZero():
		return a
	}
	return Message{Key: "%s %s", Args: []any{a, b}}
}

// Contains reports whether the English rendering contains sub, ignoring
// case.
func (m Message) Contains(sub string) bool {
	return strings.Contains(strings.ToLower(m.String()), strings.ToLower(sub))
}

type messageJSON struct {
	Key  string `json:"key"`
	Args []any  `json:"args,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Args) == 0 || m.raw {
		return json.Marshal(m.Localize(nil))
	}
	args := make([]any, len(m.Args))
	for i, a := range m.Args {
		if n, ok := a.(Message); ok && !n.raw {
			// Keep nested catalog literals as objects so they are
			// localized again after a round trip.
			a = messageJSON{Key: n.Key, Args: n.Args}
		} else if ok {
			a = n.Key
		}
		args[i] = a
	}
	return json.Marshal(messageJSON{Key: m.Key, Args: args})
}

// UnmarshalJSON implements json.Unmarshaler. A plain string is kept as a
// key, so a stored catalog literal is localized again on display.
func (m *Message) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*m = Message{Key: s}
		return nil
	}
	var obj messageJSON
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	for i, a := range obj.Args {
		// Nested messages were stored as {"key", "args"} objects.
		if mm, ok := a.(map[string]any); ok {
			if k, ok := mm["key"].(string); ok {
				nested := Message{Key: k}
				if args, ok := mm["args"].([]any); ok {
					nested.Args = args
				}
				obj.Args[i] = nested
			}
		}
	}
	*m = Message{Key: obj.Key, Args: obj.Args}
	return nil
}
