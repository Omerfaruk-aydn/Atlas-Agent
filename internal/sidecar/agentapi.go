package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Permissions a sidecar-driven conversation may run under. "auto", which
// approves every tool call, is deliberately absent: a background process must
// not be able to hand itself unlimited authority.
var allowedPermissions = map[string]bool{"readonly": true, "edits": true, "configured": true}

// DefaultPermissions is used when none is named.
const DefaultPermissions = "readonly"

// Conversation is the outcome of one agentapi call.
type Conversation struct {
	SessionID string  `json:"session_id"`
	Response  string  `json:"response"`
	Cost      float64 `json:"cost,omitempty"`
}

// Event is one recorded call, kept in the sidecar's events folder.
type Event struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"`
	SessionID string    `json:"session_id"`
	Prompt    string    `json:"prompt"`
	Response  string    `json:"response"`
	Cost      float64   `json:"cost,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// AgentAPI starts and continues agent conversations on behalf of a sidecar,
// by running the agent headlessly in the project. Calls are bounded by
// Timeout and recorded under EventsDir.
type AgentAPI struct {
	Binary      string
	Project     string
	EventsDir   string
	Permissions string
	Model       string
	Timeout     time.Duration
}

func (a AgentAPI) permissions() (string, error) {
	p := a.Permissions
	if p == "" {
		p = DefaultPermissions
	}
	if !allowedPermissions[p] {
		return "", fmt.Errorf("permissions %q is not allowed for a sidecar: use readonly, edits or configured", p)
	}
	return p, nil
}

// args builds the headless run command line, without the binary.
func (a AgentAPI) args(sessionID string) ([]string, error) {
	perm, err := a.permissions()
	if err != nil {
		return nil, err
	}
	args := []string{"--cwd", a.Project, "run", "--json", "--quiet", "--permissions", perm}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	return args, nil
}

func (a AgentAPI) run(ctx context.Context, kind, sessionID, prompt string) (Conversation, error) {
	if strings.TrimSpace(prompt) == "" {
		return Conversation{}, fmt.Errorf("a prompt is required")
	}
	args, err := a.args(sessionID)
	if err != nil {
		return Conversation{}, err
	}
	binary := a.Binary
	if binary == "" {
		if binary, err = os.Executable(); err != nil {
			return Conversation{}, err
		}
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = a.Project
	cmd.Stdin = strings.NewReader(prompt)
	// The agent runs in-process so the unattended permission policy applies.
	cmd.Env = append(os.Environ(), "ATLAS_AGENT_CLIENT_SERVER=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	conv := Conversation{SessionID: sessionID}
	var doc Conversation
	if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &doc) == nil && (doc.SessionID != "" || doc.Response != "") {
		conv = doc
	} else {
		conv.Response = strings.TrimSpace(stdout.String())
	}
	if runErr != nil {
		runErr = fmt.Errorf("%w: %s", runErr, strings.TrimSpace(lastText(stderr.String(), 400)))
	}
	a.record(Event{Time: time.Now().UTC(), Kind: kind, SessionID: conv.SessionID, Prompt: prompt, Response: conv.Response, Cost: conv.Cost, Error: errText(runErr)})
	return conv, runErr
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func lastText(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// NewConversation starts a conversation with a prompt.
func (a AgentAPI) NewConversation(ctx context.Context, prompt string) (Conversation, error) {
	return a.run(ctx, "new-conversation", "", prompt)
}

// SendMessage continues an existing conversation.
func (a AgentAPI) SendMessage(ctx context.Context, sessionID, prompt string) (Conversation, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Conversation{}, fmt.Errorf("a conversation id is required")
	}
	return a.run(ctx, "send-message", sessionID, prompt)
}

// Metadata summarizes what this sidecar has recorded for a conversation.
type Metadata struct {
	SessionID string    `json:"session_id"`
	Messages  int       `json:"messages_sent"`
	LastSeen  time.Time `json:"last_seen"`
	LastError string    `json:"last_error,omitempty"`
	Cost      float64   `json:"cost"`
}

func (a AgentAPI) record(e Event) {
	if a.EventsDir == "" {
		return
	}
	if os.MkdirAll(a.EventsDir, 0o755) != nil {
		return
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	name := fmt.Sprintf("%s-%s.json", e.Time.Format("20060102-150405.000000"), e.Kind)
	_ = os.WriteFile(filepath.Join(a.EventsDir, name), data, 0o644)
}

// GetMetadata reads the recorded events for a conversation.
func (a AgentAPI) GetMetadata(sessionID string) (Metadata, error) {
	entries, err := os.ReadDir(a.EventsDir)
	if err != nil {
		return Metadata{}, fmt.Errorf("no events recorded: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	m := Metadata{SessionID: sessionID}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(a.EventsDir, n))
		if err != nil {
			continue
		}
		var e Event
		if json.Unmarshal(data, &e) != nil || e.SessionID != sessionID {
			continue
		}
		m.Messages++
		m.LastSeen, m.Cost, m.LastError = e.Time, m.Cost+e.Cost, e.Error
	}
	if m.Messages == 0 {
		return Metadata{}, fmt.Errorf("no recorded conversation %q", sessionID)
	}
	return m, nil
}
