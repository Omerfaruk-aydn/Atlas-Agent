package permission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type headlessKey struct{}

var ErrPromptUnavailable = errors.New("permission requires user interaction; configure an explicit allowlist before unattended execution")

// WithoutPrompts preserves existing grants but rejects new interactive waits.
func WithoutPrompts(ctx context.Context) context.Context {
	return context.WithValue(ctx, headlessKey{}, true)
}

// HeadlessPolicy is an explicit, reviewable CI grant list. An omitted list
// grants nothing; wildcard grants require an explicit opt-in.
type HeadlessPolicy struct {
	AllowedTools []string `json:"allowed_tools"`
	AllowAll     bool     `json:"allow_all,omitempty"`
}

func LoadHeadlessPolicy(path string) (HeadlessPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return HeadlessPolicy{}, fmt.Errorf("read headless permission policy: %w", err)
	}
	var policy HeadlessPolicy
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return HeadlessPolicy{}, fmt.Errorf("parse headless permission policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return HeadlessPolicy{}, errors.New("headless permission policy must contain one JSON object")
	}
	for _, grant := range policy.AllowedTools {
		if strings.TrimSpace(grant) == "" {
			return HeadlessPolicy{}, errors.New("headless permission grants cannot be empty")
		}
		if grant == "*" && !policy.AllowAll {
			return HeadlessPolicy{}, errors.New("wildcard permission requires allow_all: true")
		}
	}
	return policy, nil
}
