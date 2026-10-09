package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

const (
	toolDiscoveryVersion = 2
	// maxDiscoveredTools bounds the schemas a long session accumulates.
	// The least recently used discoveries are dropped first.
	maxDiscoveredTools = 32
	// discoveryIdleTurns drops a discovery that has been neither searched
	// for nor used for this many user turns. Eviction only happens at the
	// start of a turn, so a tool never disappears in the middle of the work
	// that loaded it.
	discoveryIdleTurns     = 20
	discoveryWriteAttempts = 4
)

var validDiscoveredName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

type discoveredTool struct {
	Name    string `json:"name"`
	Touched int64  `json:"touched"`
}

// discoveryRecord is the per-session discovery state. It holds tool names
// only; it is never a permission grant. Every step intersects it with the
// live authorized palette, so a tool or MCP server that has since been
// disabled cannot come back through an old record.
type discoveryRecord struct {
	Version int              `json:"version"`
	Turn    int64            `json:"turn"`
	Tools   []discoveredTool `json:"tools"`
}

func discoveryNamespace(session string) string {
	return "tool-discovery-" + engineering.Hash(session)
}

// readDiscovery returns the session's record and its revision. A record that
// cannot be decoded is treated as empty: falling back to the core palette is
// safe, while failing the turn or opening every tool would not be. The next
// discovery overwrites it.
func readDiscovery(ctx context.Context, store *engineering.Store, session string) (discoveryRecord, uint64, bool, error) {
	record, data, err := store.ReadRecord(ctx, discoveryNamespace(session), "enabled")
	if errors.Is(err, os.ErrNotExist) {
		return discoveryRecord{Version: toolDiscoveryVersion}, 0, false, nil
	}
	if err != nil {
		return discoveryRecord{}, 0, false, err
	}
	var current discoveryRecord
	if err := json.Unmarshal(data, &current); err != nil || current.Version != toolDiscoveryVersion {
		// Version 1 stored a bare sorted name list.
		var legacy []string
		if legacyErr := json.Unmarshal(data, &legacy); legacyErr == nil {
			current = discoveryRecord{Version: toolDiscoveryVersion}
			for _, name := range legacy {
				current.Tools = append(current.Tools, discoveredTool{Name: name})
			}
		} else {
			slog.Warn("Ignoring unreadable tool discovery record; using the core palette", "bytes", len(data))
			return discoveryRecord{Version: toolDiscoveryVersion}, record.Revision, true, nil
		}
	}
	current.Tools = normalizeDiscovered(current.Tools)
	return current, record.Revision, true, nil
}

func normalizeDiscovered(in []discoveredTool) []discoveredTool {
	out := make([]discoveredTool, 0, len(in))
	for _, tool := range in {
		if !validDiscoveredName.MatchString(tool.Name) || tool.Touched < 0 {
			continue
		}
		if i := slices.IndexFunc(out, func(t discoveredTool) bool { return t.Name == tool.Name }); i >= 0 {
			out[i].Touched = max(out[i].Touched, tool.Touched)
			continue
		}
		out = append(out, tool)
	}
	// Most recently touched first, then by name, so the bound keeps the
	// tools the session is actually using.
	slices.SortFunc(out, func(a, b discoveredTool) int {
		if a.Touched != b.Touched {
			if a.Touched > b.Touched {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out[:min(len(out), maxDiscoveredTools)]
}

// updateDiscovery applies change with optimistic concurrency. Parallel tool
// calls in one step may both record discoveries; a conflict re-reads and
// reapplies instead of failing the tool call.
func updateDiscovery(ctx context.Context, store *engineering.Store, session string, createIfMissing bool, change func(*discoveryRecord) bool) error {
	if store == nil || session == "" {
		return nil
	}
	for range discoveryWriteAttempts {
		current, revision, exists, err := readDiscovery(ctx, store, session)
		if err != nil {
			return err
		}
		if !exists && !createIfMissing {
			return nil
		}
		if !change(&current) {
			return nil
		}
		current.Version = toolDiscoveryVersion
		current.Tools = normalizeDiscovered(current.Tools)
		encoded, err := json.Marshal(current)
		if err != nil {
			return err
		}
		_, err = store.PutRecordStrict(ctx, discoveryNamespace(session), "enabled", revision, encoded)
		if errors.Is(err, engineering.ErrRecordRevisionConflict) {
			continue
		}
		return err
	}
	return fmt.Errorf("tool discovery record kept changing; try again")
}

// DiscoverTools records that names should be loaded for this session from
// the next model step on. Recording grants nothing: names outside the live
// authorized palette are ignored when the palette is built.
func DiscoverTools(ctx context.Context, store *engineering.Store, session string, names []string) error {
	var wanted []string
	for _, name := range names {
		if validDiscoveredName.MatchString(name) && !slices.Contains(CoreToolNames, name) && !slices.Contains(wanted, name) {
			wanted = append(wanted, name)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	return updateDiscovery(ctx, store, session, true, func(record *discoveryRecord) bool {
		changed := false
		for _, name := range wanted {
			i := slices.IndexFunc(record.Tools, func(t discoveredTool) bool { return t.Name == name })
			switch {
			case i < 0:
				record.Tools = append(record.Tools, discoveredTool{Name: name, Touched: record.Turn})
				changed = true
			case record.Tools[i].Touched < record.Turn:
				record.Tools[i].Touched = record.Turn
				changed = true
			}
		}
		return changed
	})
}

// BeginToolDiscoveryTurn advances the session's turn counter and drops
// discoveries idle for discoveryIdleTurns turns. Sessions that never
// discovered anything are not written.
func BeginToolDiscoveryTurn(ctx context.Context, store *engineering.Store, session string) error {
	return updateDiscovery(ctx, store, session, false, func(record *discoveryRecord) bool {
		record.Turn++
		record.Tools = slices.DeleteFunc(record.Tools, func(t discoveredTool) bool {
			return record.Turn-t.Touched > discoveryIdleTurns
		})
		return true
	})
}

// DiscoveredToolNames lists the session's recorded discoveries.
func DiscoveredToolNames(ctx context.Context, store *engineering.Store, session string) ([]string, error) {
	if store == nil || session == "" {
		return nil, nil
	}
	record, _, _, err := readDiscovery(ctx, store, session)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(record.Tools))
	for _, tool := range record.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

// DeferralActive reports whether a palette can defer schemas: discovery
// needs tool_search to be part of the authorized palette.
func DeferralActive(store *engineering.Store, session string, palette []fantasy.AgentTool) bool {
	return store != nil && session != "" && slices.ContainsFunc(palette, func(t fantasy.AgentTool) bool {
		return t.Info().Name == ToolSearchToolName
	})
}

// SplitDeferredPalette divides the live authorized palette into tools whose
// schemas are sent this step (core, pinned and discovered) and the rest.
// Discoveries are intersected with the palette on every call, so a record
// can neither restore a disabled tool nor add one that was never allowed.
func SplitDeferredPalette(ctx context.Context, store *engineering.Store, session string, palette []fantasy.AgentTool, pinned []string) (loaded, deferred []fantasy.AgentTool, err error) {
	if !DeferralActive(store, session, palette) {
		return palette, nil, nil
	}
	discovered, err := DiscoveredToolNames(ctx, store, session)
	if err != nil {
		return nil, nil, err
	}
	wanted := func(name string) bool {
		return slices.Contains(CoreToolNames, name) || slices.Contains(pinned, name) || slices.Contains(discovered, name)
	}
	companions := map[string]bool{}
	for _, tool := range palette {
		if name := tool.Info().Name; wanted(name) {
			for _, companion := range deferredCompanions[name] {
				companions[companion] = true
			}
		}
	}
	for _, tool := range palette {
		name := tool.Info().Name
		if wanted(name) || companions[name] {
			loaded = append(loaded, tool)
		} else {
			deferred = append(deferred, tool)
		}
	}
	return loaded, deferred, nil
}

// deferredCompanions are advertised whenever their primary tool is. A
// multi-step browser or desktop task needs the pipeline that orders and
// verifies its steps; loading only the primary tool left that capability
// invisible, so every step became its own model turn.
var deferredCompanions = map[string][]string{
	ComputerToolName: {ToolPipelineName},
	BrowserToolName:  {ToolPipelineName},
}

// DeferredToolLoader loads a deferred tool after the model called it without
// its schema. It reports whether this was the first attempt for that tool in
// the current turn.
type DeferredToolLoader func(ctx context.Context, name string) (first bool, err error)

// deferredToolStub stands in, unadvertised, for an authorized tool whose
// schema was not sent this step. Calling it never runs the real tool: a
// call made without the schema may carry guessed arguments, and replaying a
// side-effecting call behind the model's back would be unsafe. Instead the
// stub loads the schema for the next step and tells the model to call again.
type deferredToolStub struct {
	name string
	load DeferredToolLoader
}

// NewDeferredToolStub builds the placeholder for one deferred tool.
func NewDeferredToolStub(name string, load DeferredToolLoader) fantasy.AgentTool {
	return &deferredToolStub{name: name, load: load}
}

func (s *deferredToolStub) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: s.name, Description: "Deferred tool placeholder.", Parameters: map[string]any{}, Required: []string{}}
}

func (s *deferredToolStub) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (s *deferredToolStub) SetProviderOptions(fantasy.ProviderOptions) {}

func (s *deferredToolStub) Run(ctx context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	first, err := s.load(ctx, s.name)
	switch {
	case err != nil:
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Tool %q is authorized but its schema could not be loaded, so this call was NOT executed: %v", s.name, err)), nil
	case !first:
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Tool %q was already requested without its schema this turn and was NOT executed. Do not repeat this call; use tool_search to check what is available.", s.name)), nil
	default:
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Tool %q is authorized, but its schema was not loaded for this step, so this call was NOT executed and had no effect. Its schema is now loaded: on your next step call %s again with arguments that follow that schema. Use tool_search to load several related tools at once.", s.name, s.name)), nil
	}
}
