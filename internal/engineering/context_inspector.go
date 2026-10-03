package engineering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

type ContextEntry struct {
	ParentID        string `json:"parent_id,omitempty"`
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Bytes           int    `json:"bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Fingerprint     string `json:"fingerprint"`
	Excluded        bool   `json:"excluded,omitempty"`
}

// ContextManifest records metadata, never raw prompts, credentials or outputs.
// Estimates include serialized message/schema overhead and exclude image cost.
type ContextManifest struct {
	Truncated       bool           `json:"truncated,omitempty"`
	SessionID       string         `json:"session_id"`
	Model           string         `json:"model"`
	Step            int            `json:"step"`
	CapturedAt      int64          `json:"captured_at"`
	EstimatedTokens int            `json:"estimated_tokens"`
	Entries         []ContextEntry `json:"entries"`
}

type ContextPreferences struct {
	PinnedPaths     []string `json:"pinned_paths,omitempty"`
	ExcludedResults []string `json:"excluded_results,omitempty"`
}

func contextNamespace(id string) string { return "model-context-" + Hash(id) }

func (s *Store) ReadContext(ctx context.Context, id string) (ContextManifest, ContextPreferences, error) {
	var manifest ContextManifest
	var prefs ContextPreferences
	for key, target := range map[string]any{"manifest": &manifest, "preferences": &prefs} {
		_, data, err := s.ReadRecord(ctx, contextNamespace(id), key)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return manifest, prefs, err
		}
		if err = json.Unmarshal(data, target); err != nil {
			return manifest, prefs, err
		}
	}
	if len(prefs.PinnedPaths) > 16 || len(prefs.ExcludedResults) > 128 || len(manifest.Entries) > 2048 {
		return manifest, prefs, fmt.Errorf("context record exceeds bounds")
	}
	return manifest, prefs, nil
}

func (s *Store) SaveContextManifest(ctx context.Context, id string, manifest ContextManifest) error {
	if len(manifest.Entries) > 2048 {
		return fmt.Errorf("context manifest exceeds 2048 entries")
	}
	release, err := s.WorkflowLock(ctx, "context:"+id)
	if err != nil {
		return err
	}
	defer release()
	record, _, err := s.ReadRecord(ctx, contextNamespace(id), "manifest")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	_, err = s.PutRecordStrict(ctx, contextNamespace(id), "manifest", record.Revision, data)
	return err
}

func (s *Store) ContextControl(ctx context.Context, root, id, action, value string) error {
	if value == "" || len(value) > 512 {
		return fmt.Errorf("context control requires a bounded path or tool result ID")
	}
	release, err := s.WorkflowLock(ctx, "context:"+id)
	if err != nil {
		return err
	}
	defer release()
	manifest, prefs, err := s.ReadContext(ctx, id)
	if err != nil {
		return err
	}
	switch action {
	case "context_pin":
		if _, err := ReadProjectEvidence(ctx, root, value); err != nil {
			return err
		}
		if !slices.Contains(prefs.PinnedPaths, value) {
			prefs.PinnedPaths = append(prefs.PinnedPaths, value)
		}
	case "context_unpin":
		prefs.PinnedPaths = slices.DeleteFunc(prefs.PinnedPaths, func(v string) bool { return v == value })
	case "context_exclude":
		found := false
		for _, entry := range manifest.Entries {
			if entry.Kind == "tool-result" && entry.ID == value {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("only an observed tool result can be excluded; mandatory rules and user messages remain intact")
		}
		if !slices.Contains(prefs.ExcludedResults, value) {
			prefs.ExcludedResults = append(prefs.ExcludedResults, value)
		}
	case "context_include":
		prefs.ExcludedResults = slices.DeleteFunc(prefs.ExcludedResults, func(v string) bool { return v == value })
	default:
		return fmt.Errorf("unknown context control")
	}
	if len(prefs.PinnedPaths) > 16 || len(prefs.ExcludedResults) > 128 {
		return fmt.Errorf("context selection limit reached (16 files, 128 tool results)")
	}
	record, _, err := s.ReadRecord(ctx, contextNamespace(id), "preferences")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = s.PutRecordStrict(ctx, contextNamespace(id), "preferences", record.Revision, data)
	return err
}
