package permission

import (
	"context"
	"errors"
)

type headlessKey struct{}

var ErrPromptUnavailable = errors.New("permission requires user interaction; configure an explicit allowlist before unattended execution")

// WithoutPrompts preserves existing grants but rejects new interactive waits.
func WithoutPrompts(ctx context.Context) context.Context {
	return context.WithValue(ctx, headlessKey{}, true)
}
