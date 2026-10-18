package agent

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

//go:embed templates/desktop_protocol.md
var desktopProtocol string

//go:embed templates/tool_routing.md
var toolRoutingProtocol string

//go:embed templates/browser_protocol.md
var browserProtocol string

// desktopProtocolTools are the tools whose use the desktop protocol governs.
// The pipeline alone (browser work) runs no desktop recipe without the
// computer tool, so it does not bring the desktop procedures.
var desktopProtocolTools = []string{"computer"}

// maxIndexedNamesPerCategory keeps the capability index short even with
// large MCP servers; the rest stay reachable through tool_search.
const maxIndexedNamesPerCategory = 24

// toolDeferral is one turn's view of deferred tool schemas. A nil value
// means every authorized schema is sent on every step.
type toolDeferral struct {
	sessionID string
	pinned    []string
	load      func(context.Context, []string) error

	mu       sync.Mutex
	attempts map[string]int
}

func (a *sessionAgent) deferToolSchemas() bool {
	return a.engineering != nil && a.taskContextConfig != nil && a.taskContextConfig.Config().Options.DeferToolSchemasEnabled()
}

// beginToolDeferral prepares deferral for one turn: it ages old discoveries,
// preloads tools the request explicitly names and pins tools the session
// state already depends on. Failures to age or preload are logged and do
// not block the turn; tool_search remains available either way.
func (a *sessionAgent) beginToolDeferral(ctx context.Context, call SessionAgentCall, current session.Session, palette []fantasy.AgentTool, pendingOperations bool) *toolDeferral {
	if !a.deferToolSchemas() || !tools.DeferralActive(a.engineering, call.SessionID, palette) {
		return nil
	}
	d := &toolDeferral{
		sessionID: call.SessionID,
		attempts:  map[string]int{},
		load: func(ctx context.Context, names []string) error {
			return tools.DiscoverTools(ctx, a.engineering, call.SessionID, names)
		},
	}
	if err := tools.BeginToolDiscoveryTurn(ctx, a.engineering, call.SessionID); err != nil {
		slog.Warn("Failed to age tool discoveries", "error", err)
	}
	if intent := tools.IntentTools(call.Prompt); len(intent) > 0 {
		if err := d.load(ctx, paletteSubset(palette, intent)); err != nil {
			slog.Warn("Failed to preload requested tools", "error", err)
		}
	}
	if len(current.Todos) > 0 {
		d.pinned = append(d.pinned, "todos")
	}
	if strings.TrimSpace(current.Goal) != "" {
		d.pinned = append(d.pinned, "goal")
	}
	if pendingOperations {
		d.pinned = append(d.pinned, "workflow")
	}
	return d
}

func paletteSubset(palette []fantasy.AgentTool, names []string) []string {
	var out []string
	for _, tool := range palette {
		if slices.Contains(names, tool.Info().Name) {
			out = append(out, tool.Info().Name)
		}
	}
	return out
}

// capabilityIndex lists authorized tools whose schemas are loaded on demand.
// It names every non-core tool, loaded or not, so it stays identical across
// the steps of a turn and does not disturb prompt caching.
func (d *toolDeferral) capabilityIndex(palette []fantasy.AgentTool) string {
	if d == nil {
		return ""
	}
	groups := map[string][]string{}
	hints := map[string]string{}
	for _, tool := range palette {
		info := tool.Info()
		name := info.Name
		if slices.Contains(tools.CoreToolNames, name) || slices.Contains(d.pinned, name) {
			continue
		}
		category := tools.ToolCategory(name)
		groups[category] = append(groups[category], name)
		hints[name] = tools.ToolHint(name, info.Description)
	}
	if len(groups) == 0 {
		return ""
	}
	categories := make([]string, 0, len(groups))
	for category := range groups {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	var b strings.Builder
	b.WriteString("\n\n<deferred_tools>\nThe tools above the conversation start with core schemas only. These further tools are enabled for this session; their schemas load on demand. Each name is followed by when it helps; reach for one whenever it fits the work better than a shell command:\n")
	for _, category := range categories {
		names := groups[category]
		slices.Sort(names)
		shown := names[:min(len(names), maxIndexedNamesPerCategory)]
		entries := make([]string, 0, len(shown))
		for _, name := range shown {
			entry := name
			if hint := hints[name]; hint != "" {
				entry += " (" + hint + ")"
			}
			entries = append(entries, entry)
		}
		fmt.Fprintf(&b, "- %s: %s", category, strings.Join(entries, "; "))
		if hidden := len(names) - len(shown); hidden > 0 {
			fmt.Fprintf(&b, " (+%d more, use tool_search)", hidden)
		}
		b.WriteString("\n")
	}
	b.WriteString(`To use one, first call tool_search with exact names ({"names":["browser"]}) or a short capability query ({"query":"browser screenshot"}); its full schema is callable from your next step. Load several related tools in one call when a task needs them. Loading runs nothing and grants no permission: approvals, hooks and workspace limits still apply. Never guess the arguments of a tool whose schema you have not seen.` + "\n</deferred_tools>")
	return b.String()
}

// stepTools splits the live palette for one step. The advertised tools carry
// full schemas; every other authorized tool is represented by an
// unadvertised stub so a call made without its schema loads it instead of
// running. active lists the advertised names.
func (d *toolDeferral) stepTools(ctx context.Context, a *sessionAgent, palette []fantasy.AgentTool) (advertised, stubs []fantasy.AgentTool, err error) {
	if d == nil {
		return palette, nil, nil
	}
	if a.engineering != nil && !a.isSubAgent {
		st, readErr := a.engineering.Read(ctx, engineering.GetScope(ctx, d.sessionID).SessionID)
		if readErr != nil {
			return nil, nil, readErr
		}
		if names := workCapabilityNames(st, tools.ScopeToolsForDesktop(ctx, palette)); len(names) > 0 {
			if err := d.load(ctx, names); err != nil {
				return nil, nil, err
			}
		}
	}
	loaded, deferred, err := tools.SplitDeferredPalette(ctx, a.engineering, d.sessionID, palette, d.pinned)
	if err != nil {
		return nil, nil, err
	}
	for _, tool := range deferred {
		stubs = append(stubs, tools.NewDeferredToolStub(tool.Info().Name, d.loadAfterUnloadedCall))
	}
	return loaded, stubs, nil
}

// loadAfterUnloadedCall loads a tool the model called without its schema.
// It allows one such recovery per tool per turn, so a model that keeps
// calling the same unavailable tool gets a final refusal instead of a loop.
func (d *toolDeferral) loadAfterUnloadedCall(ctx context.Context, name string) (bool, error) {
	d.mu.Lock()
	d.attempts[name]++
	first := d.attempts[name] == 1
	d.mu.Unlock()
	if !first {
		return false, nil
	}
	return true, d.load(ctx, []string{name})
}

// touchUsed keeps tools the model actually called from ageing out.
func (d *toolDeferral) touchUsed(ctx context.Context, steps []fantasy.StepResult, advertised []fantasy.AgentTool) {
	if d == nil || len(steps) == 0 {
		return
	}
	var used []string
	for _, call := range steps[len(steps)-1].Content.ToolCalls() {
		if !slices.Contains(used, call.ToolName) && slices.ContainsFunc(advertised, func(t fantasy.AgentTool) bool { return t.Info().Name == call.ToolName }) {
			used = append(used, call.ToolName)
		}
	}
	if len(used) > 0 {
		if err := d.load(ctx, used); err != nil {
			slog.Warn("Failed to refresh used tool discoveries", "error", err)
		}
	}
}

func paletteNames(ts []fantasy.AgentTool) []string {
	names := make([]string, 0, len(ts))
	for _, tool := range ts {
		names = append(names, tool.Info().Name)
	}
	return names
}

// withToolProtocols attaches procedures that belong to specific tools right
// after the leading system messages, only while those tools are advertised.
// With deferral disabled the tools are always advertised, which preserves
// the previous always-on guidance.
func withToolProtocols(messages []fantasy.Message, advertised []string) []fantasy.Message {
	if len(advertised) == 0 {
		return messages
	}
	at := 0
	for at < len(messages) && messages[at].Role == fantasy.MessageRoleSystem {
		at++
	}
	guidance := "<tool_protocol name=\"routing\">\n" + toolRoutingProtocol + "</tool_protocol>"
	if slices.Contains(advertised, "browser") {
		guidance += "\n<tool_protocol name=\"browser\">\n" + browserProtocol + "</tool_protocol>"
	}
	if slices.ContainsFunc(desktopProtocolTools, func(name string) bool { return slices.Contains(advertised, name) }) {
		guidance += "\n<tool_protocol name=\"desktop\">\n" + desktopProtocol + "</tool_protocol>"
	}
	seen := map[string]bool{}
	for _, name := range advertised {
		category := tools.ToolCategory(name)
		if seen[category] {
			continue
		}
		seen[category] = true
		if rule := taskCategoryGuidance[category]; rule != "" {
			guidance += "\n" + rule
		}
	}
	return slices.Insert(slices.Clone(messages), at, fantasy.NewSystemMessage(guidance))
}

var taskCategoryGuidance = map[string]string{
	"git":      "Git: inspect scoped status/diff and preserve unrelated edits; never infer commit, push or merge success without evidence.",
	"testing":  "Tests: inspect the exit status and actual assertions/output; report skipped checks separately from passed checks.",
	"code":     "Code intelligence: use project_map or semantic/LSP tools when loaded; follow symbol evidence into relevant source files before claiming a dependency or complete architecture.",
	"files":    "Files: use view/grep/glob/ls for scoped discovery and edit/write for changes; preserve unrelated content and exclude unrelated credentials.",
	"shell":    "Shell: respect the host OS and shell quoting, verify available executables, and monitor background jobs through job_output rather than launching duplicate work.",
	"mcp":      "MCP: use the loaded connector schema and returned resource identifiers; distinguish disconnected services from missing resources without accessing unrelated account files.",
	"memory":   "Memory: search relevant records, preserve provenance and freshness, and distinguish remembered facts from current observations.",
	"web":      "Web: use fetch for static source reading and browser for interactive page tasks; attribute current claims to the retrieved source.",
	"config":   "Configuration: inspect exact provider/model/setting identifiers through Atlas tools; change only requested settings and protect credential values.",
	"pipeline": "Pipeline: use the documented steps and dependencies; stop on failed prerequisites and never treat partial completion as workflow success.",
	"agents":   "Agents: delegate only when authorized and useful, provide concrete scope and acceptance criteria, and verify returned evidence before integrating results.",
	"planning": "Planning: track meaningful outcomes and unresolved blockers; update completion only from evidence, not from attempted calls.",
	"workflow": "Workflow: resume from recorded checkpoints, respect cancellation and question/permission pauses, and do not replay completed side effects.",
	"quality":  "Quality: scope audits to requested code and report reproducible findings; do not collect credential values as troubleshooting evidence.",
	"docs":     "Documents: retain source locations and distinguish extracted content from inferred structure; verify any generated artifact before reporting delivery.",
	"ui":       "UI: inspect relevant rendered state, ground interaction targets in fresh observations, and verify requested visual behavior rather than claiming quality from code alone.",
	"platform": "Platform: retain task ownership and persisted progress, respect secret boundaries, and verify scheduler/job state before claiming completion.",
}

// cacheMarkedTool carries the prompt-cache breakpoint for the last advertised
// tool of a step without mutating the shared tool instance, whose position
// in the palette changes as schemas are loaded.
type cacheMarkedTool struct {
	fantasy.AgentTool
	options fantasy.ProviderOptions
}

func (t cacheMarkedTool) ProviderOptions() fantasy.ProviderOptions {
	merged := fantasy.ProviderOptions{}
	for key, value := range t.AgentTool.ProviderOptions() {
		merged[key] = value
	}
	for key, value := range t.options {
		merged[key] = value
	}
	return merged
}

func markLastToolForCache(ts []fantasy.AgentTool, options fantasy.ProviderOptions) []fantasy.AgentTool {
	if len(ts) == 0 || len(options) == 0 {
		return ts
	}
	out := slices.Clone(ts)
	out[len(out)-1] = cacheMarkedTool{AgentTool: out[len(out)-1], options: options}
	return out
}
