package prompt

import (
	"context"
	"embed"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const ProtocolVersion = "atlas-prompt-v2"

//go:embed protocols/*.md
var protocols embed.FS

var protocolOrder = []string{"large_project", "architecture", "migration", "debug", "research", "ui", "platform"}

type protocolKey struct{}

// WithProtocols freezes the selected guidance for this request.
func WithProtocols(ctx context.Context, ids []string) context.Context {
	return context.WithValue(ctx, protocolKey{}, slices.Clone(ids))
}

// CanonicalProtocols validates, deduplicates and orders persisted selections.
func CanonicalProtocols(ids []string) ([]string, error) {
	for _, id := range ids {
		if !slices.Contains(protocolOrder, id) {
			return nil, fmt.Errorf("unknown prompt protocol %q", id)
		}
	}
	var out []string
	for _, id := range protocolOrder {
		if slices.Contains(ids, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// SelectProtocols chooses guidance from task intent and the selected role.
// It changes neither tool access nor the model's execution permissions.
func SelectProtocols(task, role string) []string {
	words := strings.FieldsFunc(strings.ToLower(task+" "+role), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	has := func(keys ...string) bool {
		for _, key := range keys {
			if slices.Contains(words, key) {
				return true
			}
		}
		return false
	}
	selected := map[string]bool{
		"large_project": has("platform", "modules", "modüller", "modülleri", "orchestrate", "workflow", "planner", "planning", "roadmap", "mimari", "architect", "architecture") || len(task) > 1600,
		"architecture":  has("architect", "architecture", "mimari", "mimarisini", "contract", "contracts", "sözleşme", "api", "backend"),
		"migration":     has("migration", "migrate", "migrasyon", "schema", "şema", "backfill", "upgrade", "sqlite", "database", "veritabanı"),
		"debug":         has("debug", "diagnose", "bug", "regression", "hata", "hatayı", "hataları", "deadlock", "race"),
		"research":      has("research", "investigate", "araştır", "araştırma", "kaynak", "sources"),
		"ui":            has("ui", "ux", "frontend", "tui", "arayüz", "arayüzü", "tasarım", "tasarımı", "responsive", "animation", "animasyon", "swiftui"),
		"platform":      has("agent_jobs", "task_board", "source_memory", "tool_pipeline", "vault", "heartbeat", "cron", "kasa", "hedef", "goal", "pipeline"),
	}
	var out []string
	for _, id := range protocolOrder {
		if selected[id] {
			out = append(out, id)
		}
	}
	return out
}

// RenderProtocols resolves only known embedded modules in stable order.
func RenderProtocols(ids []string) (string, error) {
	var out strings.Builder
	fmt.Fprintf(&out, "<prompt_recipe version=%q>\n", ProtocolVersion)
	for _, id := range protocolOrder {
		if !slices.Contains(ids, id) {
			continue
		}
		data, err := protocols.ReadFile("protocols/" + id + ".md")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "<task_protocol name=%q>\n%s\n</task_protocol>\n", id, data)
	}
	for _, id := range ids {
		if !slices.Contains(protocolOrder, id) {
			return "", fmt.Errorf("unknown prompt protocol %q", id)
		}
	}
	out.WriteString("</prompt_recipe>")
	return out.String(), nil
}
