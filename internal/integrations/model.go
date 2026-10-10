// Package integrations is the single source of truth for what Atlas can
// reach: model providers, signed-in accounts, MCP servers, local tools,
// local app runtimes and the local apps Atlas manages. Every item reports
// where its configuration comes from, what it can really do, and the result
// of the last check. Being configured and being usable are separate states:
// an item is only "ready" after a live check or a live runtime state said so.
//
// The /integrations screen and the agent's integrations tool both read this
// service, so the user and the model never see two different answers.
package integrations

import (
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/i18n"
)

// Kind groups integrations by what they are.
type Kind string

// Kinds.
const (
	KindProvider   Kind = "provider"
	KindAccount    Kind = "account"
	KindMCP        Kind = "mcp"
	KindTool       Kind = "tool"
	KindApp        Kind = "app"
	KindDependency Kind = "dependency"
)

// Kinds lists every kind in display order.
func Kinds() []Kind {
	return []Kind{KindProvider, KindAccount, KindMCP, KindTool, KindDependency, KindApp}
}

// Status is the user-facing health of one integration.
type Status string

// Statuses. Checking is transient and never persisted.
const (
	StatusNotConfigured     Status = "not_configured"
	StatusUnchecked         Status = "unchecked"
	StatusChecking          Status = "checking"
	StatusReady             Status = "ready"
	StatusNeedsLogin        Status = "needs_login"
	StatusMissingDependency Status = "missing_dependency"
	StatusConnectionError   Status = "connection_error"
	StatusLimited           Status = "limited"
	StatusDisabled          Status = "disabled"
)

// NeedsAttention reports whether the status asks the user to do something.
func (s Status) NeedsAttention() bool {
	switch s {
	case StatusNeedsLogin, StatusMissingDependency, StatusConnectionError, StatusLimited:
		return true
	}
	return false
}

// AuthKind says how an integration authenticates.
type AuthKind string

// Authentication kinds.
const (
	AuthNone    AuthKind = "none"
	AuthAPIKey  AuthKind = "api_key"
	AuthOAuth   AuthKind = "oauth"
	AuthHeaders AuthKind = "headers"
	AuthEnv     AuthKind = "environment"
)

// Action is an operation an adapter really supports for one item.
type Action string

// Actions. An item lists only the ones its adapter implements for it.
const (
	// ActionTest runs the cheap, side-effect free check.
	ActionTest Action = "test"
	// ActionConfigure opens the existing setup flow (e.g. /model).
	ActionConfigure Action = "configure"
	// ActionLogin starts the existing sign-in flow.
	ActionLogin Action = "login"
	// ActionEnable and ActionDisable flip the disabled flag in the file
	// that defines the item; credentials are kept.
	ActionEnable  Action = "enable"
	ActionDisable Action = "disable"
	// ActionForgetCredentials removes saved credentials but keeps the
	// rest of the configuration.
	ActionForgetCredentials Action = "forget_credentials"
	// ActionRemove deletes the item's configuration entry.
	ActionRemove Action = "remove"
	// ActionOpenApp jumps to the local app's detail in /app.
	ActionOpenApp Action = "open_app"
)

// Destructive reports whether the action changes or deletes saved state and
// must be confirmed by the user first.
func (a Action) Destructive() bool {
	switch a {
	case ActionDisable, ActionForgetCredentials, ActionRemove:
		return true
	}
	return false
}

// Source says where an item's configuration lives and whether Atlas may
// write it.
type Source struct {
	// Origin is a short label: "atlas.json", "atlasrc", "environment",
	// "built-in catalog", "PATH", "app registry".
	Origin string `json:"origin"`
	// Scope is global, workspace, system or user.
	Scope string `json:"scope"`
	// Path is the defining file, when there is one.
	Path string `json:"path,omitempty"`
	// Editable is true only when the definition lives in a file Atlas
	// writes (the global data atlas.json or the workspace .atlas/atlas.json).
	Editable bool `json:"editable"`
	// ConfigKey is the JSON path of the entry in Path, when editable.
	ConfigKey string `json:"config_key,omitempty"`
}

// PreviousCheck is a result recorded before Atlas last restarted. It is
// history, never the current state.
type PreviousCheck struct {
	Status    Status       `json:"status"`
	CheckedAt time.Time    `json:"checked_at"`
	Detail    i18n.Message `json:"detail,omitzero"`
}

// Item is one integration as the UI and the agent see it. It never carries
// secrets: errors and details are sanitized before they are stored.
type Item struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Kind         Kind           `json:"kind"`
	Description  i18n.Message   `json:"description,omitzero"`
	Source       Source         `json:"source"`
	Capabilities []i18n.Message `json:"capabilities,omitempty"`
	Auth         AuthKind       `json:"auth"`
	Status       Status         `json:"status"`
	Detail       i18n.Message   `json:"detail,omitzero"`
	CheckedAt    time.Time      `json:"checked_at,omitzero"`
	LastError    i18n.Message   `json:"last_error,omitzero"`
	Remedy       i18n.Message   `json:"remedy,omitzero"`
	Actions      []Action       `json:"actions,omitempty"`
	CheckNote    i18n.Message   `json:"check_note,omitzero"`
	Previous     *PreviousCheck `json:"previous,omitempty"`
	// AppID links an app item (or a runtime dependency) to /app.
	AppID string `json:"app_id,omitempty"`
	// Live is true when Status comes from a running subsystem (MCP client
	// states, the local app process manager) rather than from a check.
	Live bool `json:"live,omitempty"`
	// Stale explains why the last check result is not shown as current.
	Stale i18n.Message `json:"stale,omitzero"`

	// fingerprint identifies the configuration a check result belongs to.
	// It is salted per process and never persisted or shown.
	fingerprint string
}

// Supports reports whether the item lists action a.
func (i Item) Supports(a Action) bool {
	for _, x := range i.Actions {
		if x == a {
			return true
		}
	}
	return false
}

// CheckResult is what an adapter's check observed.
type CheckResult struct {
	Status       Status
	Detail       i18n.Message
	Err          i18n.Message
	Remedy       i18n.Message
	Capabilities []i18n.Message
}

// Filter narrows a listing. Empty fields match everything.
type Filter struct {
	Kind Kind `json:"kind,omitempty"`
	// Status is a concrete status, or "attention" for every status that
	// asks the user to act.
	Status string `json:"status,omitempty"`
	Query  string `json:"query,omitempty"`
	// Configured hides catalog entries that are not set up.
	Configured bool `json:"configured,omitempty"`
}

// EventType distinguishes service events.
type EventType string

// Event types.
const (
	EventCheckStarted  EventType = "check_started"
	EventCheckFinished EventType = "check_finished"
	EventCheckDropped  EventType = "check_dropped"
	EventChanged       EventType = "changed"
)

// Event is published on the service broker.
type Event struct {
	Type EventType
	ID   string
	Item Item
}

// Summary is the compact overview handed to the model.
type Summary struct {
	Total     int            `json:"total"`
	ByKind    map[Kind]int   `json:"by_kind"`
	ByStatus  map[Status]int `json:"by_status"`
	Attention []Brief        `json:"attention,omitempty"`
	Unchecked int            `json:"unchecked"`
}

// Brief is a one-line view of an item.
type Brief struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Kind   Kind         `json:"kind"`
	Status Status       `json:"status"`
	Detail i18n.Message `json:"detail,omitzero"`
	Remedy i18n.Message `json:"remedy,omitzero"`
}
