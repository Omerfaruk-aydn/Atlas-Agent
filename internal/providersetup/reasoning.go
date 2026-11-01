package providersetup

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/catwalk"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-models/pkg/embedded"
)

// knownEfforts are the reasoning levels the rest of the app understands,
// in ascending order. Anything else a provider reports is ignored.
var knownEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// fallbackEfforts are used when a provider says a model reasons but does
// not say at which levels.
var fallbackEfforts = []string{"low", "medium", "high"}

// reasoningMeta is what one /models entry says about reasoning.
type reasoningMeta struct {
	// supported is true when the entry says the model can reason.
	supported bool
	// denied is true when the entry explicitly says it cannot.
	denied bool
	levels []string
}

// entryReasoning is the raw, shape-agnostic reasoning metadata of an entry.
type entryReasoning struct {
	SupportedParameters []string        `json:"supported_parameters"`
	Reasoning           json.RawMessage `json:"reasoning"`
	SupportsReasoning   json.RawMessage `json:"supports_reasoning"`
	CanReason           json.RawMessage `json:"can_reason"`
	Thinking            json.RawMessage `json:"thinking"`
	Capabilities        json.RawMessage `json:"capabilities"`
	Efforts             json.RawMessage `json:"reasoning_efforts"`
	Levels              json.RawMessage `json:"reasoning_levels"`
	SupportedEfforts    json.RawMessage `json:"supported_reasoning_efforts"`
}

// readReasoning extracts reasoning metadata best-effort. Shapes it does not
// recognise are treated as silence, never as support.
func readReasoning(r entryReasoning) reasoningMeta {
	var meta reasoningMeta

	for _, p := range r.SupportedParameters {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "reasoning", "reasoning_effort", "include_reasoning":
			meta.supported = true
		}
	}

	flags := []json.RawMessage{r.Reasoning, r.SupportsReasoning, r.CanReason, r.Thinking}
	if len(r.Capabilities) > 0 {
		var caps map[string]json.RawMessage
		if json.Unmarshal(r.Capabilities, &caps) == nil {
			flags = append(flags, caps["reasoning"], caps["thinking"])
		}
	}
	for _, raw := range flags {
		on, known, nested := flagValue(raw)
		if !known {
			continue
		}
		if on {
			meta.supported = true
		} else {
			meta.denied = true
		}
		meta.levels = append(meta.levels, nested...)
	}

	for _, raw := range []json.RawMessage{r.Efforts, r.Levels, r.SupportedEfforts} {
		meta.levels = append(meta.levels, stringList(raw)...)
	}

	meta.levels = cleanEfforts(meta.levels)
	if len(meta.levels) > 0 {
		meta.supported = true
	}
	if meta.supported {
		meta.denied = false
	}
	return meta
}

// flagValue reads a boolean, or an object describing a capability
// ({"supported": true, "efforts": [...]}). known is false for anything else.
func flagValue(raw json.RawMessage) (on, known bool, levels []string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, false, nil
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, true, nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return false, false, nil
	}
	for _, k := range []string{"efforts", "levels", "supported_efforts", "supported_levels"} {
		levels = append(levels, stringList(obj[k])...)
	}
	if v, ok := obj["supported"]; ok {
		if json.Unmarshal(v, &b) == nil {
			return b, true, levels
		}
	}
	if len(levels) > 0 {
		return true, true, levels
	}
	return false, false, nil
}

func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	return list
}

// cleanEfforts keeps only known levels, deduplicated and in ascending order.
func cleanEfforts(in []string) []string {
	var out []string
	for _, known := range knownEfforts {
		for _, s := range in {
			if strings.EqualFold(strings.TrimSpace(s), known) {
				out = append(out, known)
				break
			}
		}
	}
	return out
}

// defaultEffort picks medium when offered, else the middle level.
func defaultEffort(levels []string) string {
	if len(levels) == 0 {
		return ""
	}
	if slices.Contains(levels, "medium") {
		return "medium"
	}
	return levels[len(levels)/2]
}

// applyReasoning configures reasoning effort on m from provider metadata,
// falling back to the embedded catalog when the metadata is silent.
func applyReasoning(m *catwalk.Model, meta reasoningMeta) {
	switch {
	case meta.supported:
		levels := meta.levels
		if len(levels) == 0 {
			levels = slices.Clone(fallbackEfforts)
		}
		m.CanReason = true
		m.ReasoningLevels = levels
		m.DefaultReasoningEffort = defaultEffort(levels)
	case !meta.denied:
		applyCatalogReasoning(m)
	}
}

var (
	catalogOnce sync.Once
	catalogByID map[string]catwalk.Model
)

// catalogReasoning returns the embedded catalog's reasoning models by id.
// The first catalog entry for an id wins.
func catalogReasoning() map[string]catwalk.Model {
	catalogOnce.Do(func() {
		catalogByID = map[string]catwalk.Model{}
		for _, p := range embedded.GetAll() {
			for _, cm := range p.Models {
				if !cm.CanReason {
					continue
				}
				if _, dup := catalogByID[cm.ID]; !dup {
					catalogByID[cm.ID] = cm
				}
			}
		}
	})
	return catalogByID
}

// applyCatalogReasoning reuses the catalog's reasoning settings for a model
// whose id matches exactly. It never changes a model already marked as
// reasoning.
func applyCatalogReasoning(m *catwalk.Model) {
	if m.CanReason {
		return
	}
	cm, ok := catalogReasoning()[m.ID]
	if !ok {
		return
	}
	levels := keepKnown(cm.ReasoningLevels)
	if len(levels) == 0 {
		levels = slices.Clone(fallbackEfforts)
	}
	m.CanReason = true
	m.ReasoningLevels = levels
	m.DefaultReasoningEffort = cm.DefaultReasoningEffort
	if !slices.Contains(levels, m.DefaultReasoningEffort) {
		m.DefaultReasoningEffort = defaultEffort(levels)
	}
}

// reasoningCount counts models configured for reasoning effort.
func reasoningCount(models []catwalk.Model) int {
	n := 0
	for _, m := range models {
		if m.CanReason && len(m.ReasoningLevels) > 0 {
			n++
		}
	}
	return n
}

// keepKnown drops unknown and repeated levels but keeps the given order.
func keepKnown(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if slices.Contains(knownEfforts, s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
