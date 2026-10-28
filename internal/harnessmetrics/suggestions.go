package harnessmetrics

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ImprovementProposal is evidence, not an authorized harness change.
type ImprovementProposal struct {
	Cluster       string   `json:"cluster"`
	Kind          string   `json:"kind"`
	Proposal      string   `json:"proposal"`
	EvidenceCases []string `json:"evidence_cases"`
	Occurrences   int      `json:"occurrences"`
	ABRequired    bool     `json:"ab_required"`
	AutoApply     bool     `json:"auto_apply"`
}

var commandNoise = regexp.MustCompile(`(?i)\b[0-9a-f]{7,}\b|\b\d+(?:\.\d+)?(?:ms|s|m|h)?\b`)

func failureSignature(c Case) string {
	parts := append([]string{c.Reason, c.Findings}, c.Checks...)
	text := strings.ToLower(strings.Join(parts, " "))
	text = commandNoise.ReplaceAllString(text, "<value>")
	return strings.Join(strings.Fields(text), " ")
}

func failureKind(c Case) string {
	text := strings.ToLower(c.Reason + " " + c.Findings + " " + strings.Join(c.Checks, " "))
	switch {
	case strings.Contains(text, "permission") || strings.Contains(text, "denied"):
		return "permission"
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return "timeout"
	case strings.Contains(text, "lint") || strings.Contains(text, "vet"):
		return "lint"
	case strings.Contains(text, "build") || strings.Contains(text, "compile"):
		return "build"
	case strings.Contains(text, "test") || strings.Contains(text, "assert"):
		return "test"
	default:
		return "verification"
	}
}

func proposalFor(kind string) (string, string) {
	switch kind {
	case "permission":
		return "tool_hint", "Add a tool hint explaining scoped permission requests and fail-closed alternatives."
	case "timeout":
		return "default_setting", "Review timeout defaults and add bounded progress checkpoints before retrying."
	case "lint":
		return "rule", "Add a rule to run the narrow formatter and lint checks before the full contract."
	case "build":
		return "tool_hint", "Add compiler-error triage guidance: inspect the first error and its importers before editing."
	case "test":
		return "rule", "Strengthen the red-to-green rule: reproduce the failing assertion before changing implementation."
	default:
		return "prompt", "Add a verification prompt checkpoint that ties each completion claim to observed command output."
	}
}

// ProposeImprovements clusters recurring failures and emits review-only
// proposals. Cases without failure evidence never produce a suggestion.
func ProposeImprovements(cases []Case) []ImprovementProposal {
	groups := map[string][]Case{}
	for _, c := range cases {
		if c.ID == "" || strings.TrimSpace(c.Request) == "" || strings.TrimSpace(c.Reason+c.Findings+strings.Join(c.Checks, "")) == "" {
			continue
		}
		kind := failureKind(c)
		signature := kind + "\x00" + failureSignature(c)
		groups[signature] = append(groups[signature], c)
	}
	var proposals []ImprovementProposal
	for signature, group := range groups {
		if len(group) < 2 {
			continue
		}
		kind := failureKind(group[0])
		changeKind, proposal := proposalFor(kind)
		ids := make([]string, 0, len(group))
		for _, c := range group {
			ids = append(ids, c.ID)
		}
		slices.Sort(ids)
		proposals = append(proposals, ImprovementProposal{
			Cluster: engineeringHash(signature), Kind: changeKind, Proposal: proposal,
			EvidenceCases: ids, Occurrences: len(group), ABRequired: true, AutoApply: false,
		})
	}
	slices.SortFunc(proposals, func(a, b ImprovementProposal) int { return strings.Compare(a.Cluster, b.Cluster) })
	return proposals
}

// engineeringHash keeps the corpus package independent of CLI hashing.
func engineeringHash(text string) string {
	// FNV-1a is sufficient for non-security cluster identifiers.
	var hash uint64 = 14695981039346656037
	for _, b := range []byte(text) {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return fmt.Sprintf("%016x", hash)
}
