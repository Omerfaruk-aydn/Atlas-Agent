// Package remotecontrol manages the relay servers behind /rc: server
// profiles, sealed SSH secrets, host key decisions, read-only preflight
// checks, an approved installation plan, a resumable deployment and the
// separate verification of HTTPS, the relay service and the relay link.
//
// Nothing in this package exposes a secret to the model, logs or errors.
// Remote commands are fixed scripts; user input only reaches the server as
// validated file content on standard input, never as shell syntax.
package remotecontrol

import "fmt"

// Message is localizable text. Key is the English catalog source with
// printf verbs and Args fill them. Args never carry secrets.
type Message struct {
	Key  string `json:"key"`
	Args []any  `json:"args,omitempty"`
}

// Msg builds a Message. Key must be a string literal present in the i18n
// catalog; a test enforces this for every call in this package.
func Msg(key string, args ...any) Message { return Message{Key: key, Args: args} }

// IsZero reports whether the message is empty.
func (m Message) IsZero() bool { return m.Key == "" }

// String renders the English text.
func (m Message) String() string { return m.Localize(nil) }

// Localize renders the message with a translation function, such as
// common.Common.Text. A nil function renders English.
func (m Message) Localize(text func(string) string) string {
	if m.Key == "" {
		return ""
	}
	key := m.Key
	if text != nil {
		key = text(key)
	}
	if len(m.Args) == 0 {
		return key
	}
	return fmt.Sprintf(key, m.Args...)
}

// Error is an error carrying a localizable explanation and the next step a
// user can take. Its text never contains secrets.
type Error struct {
	What Message
	Next Message
	// Code classifies the failure for the UI and tests.
	Code string
}

func (e *Error) Error() string {
	if e.Next.IsZero() {
		return e.What.String()
	}
	return e.What.String() + " " + e.Next.String()
}

func newError(code string, what, next Message) *Error {
	return &Error{Code: code, What: what, Next: next}
}
