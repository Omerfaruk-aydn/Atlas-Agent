package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func DiscoverTools(ctx context.Context, store *engineering.Store, session string, infos []fantasy.ToolInfo) error {
	if store == nil || session == "" || len(infos) == 0 {
		return nil
	}
	ns := "tool-discovery-" + engineering.Hash(session)
	record, data, err := store.ReadRecord(ctx, ns, "enabled")
	var names []string
	if err == nil {
		if err := json.Unmarshal(data, &names); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, info := range infos {
		if !slices.Contains(names, info.Name) {
			names = append(names, info.Name)
		}
	}
	slices.Sort(names)
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	_, err = store.PutRecordStrict(ctx, ns, "enabled", record.Revision, encoded)
	return err
}

// DeferredPalette intersects persisted discoveries with the live allowlist.
func DeferredPalette(ctx context.Context, store *engineering.Store, session string, palette []fantasy.AgentTool) ([]fantasy.AgentTool, error) {
	if store == nil || session == "" || !slices.ContainsFunc(palette, func(t fantasy.AgentTool) bool { return t.Info().Name == ToolSearchToolName }) {
		return palette, nil
	}
	names := []string{"tool_search", "bash", "view", "edit", "multiedit", "write", "grep", "glob", "ls", "question", "todos", "agent", "workflow", "goal", "usage"}
	_, data, err := store.ReadRecord(ctx, "tool-discovery-"+engineering.Hash(session), "enabled")
	if err == nil {
		var discovered []string
		if err := json.Unmarshal(data, &discovered); err != nil {
			return nil, err
		}
		names = append(names, discovered...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	selected := make([]fantasy.AgentTool, 0, len(names))
	for _, tool := range palette {
		if slices.Contains(names, tool.Info().Name) {
			selected = append(selected, tool)
		}
	}
	return selected, nil
}
