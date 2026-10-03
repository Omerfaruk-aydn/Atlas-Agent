package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/experiments"
)

//go:embed project_selection.md
var selectionDescription string

type SelectionParams struct {
	ChangedPaths []string `json:"changed_paths,omitempty" description:"At most 64 literal workspace-relative files."`
	Query        string   `json:"query,omitempty" description:"Query words for matching source paths, packages and imports."`
	TokenBudget  int      `json:"token_budget,omitempty" description:"Context estimate budget 128-16000; default 2000."`
}

type selectedFile struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
	Content     string `json:"content,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
	Score       int    `json:"score"`
}

func NewSelectionTools(root string, store *engineering.Store) []fantasy.AgentTool {
	var tools []fantasy.AgentTool
	for _, name := range []string{"context_select", "test_select"} {
		tools = append(tools, fantasy.NewAgentTool(name, selectionDescription, func(ctx context.Context, p SelectionParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if p.TokenBudget == 0 {
				p.TokenBudget = 2000
			}
			if p.TokenBudget < 128 || p.TokenBudget > 16000 || len(p.ChangedPaths) > 64 || len(p.Query) > 256 {
				return fantasy.NewTextErrorResponse("invalid context budget, query or changed path count"), nil
			}
			changed := map[string]bool{}
			dirs := map[string]bool{}
			for _, path := range p.ChangedPaths {
				if _, err := engineering.ReadProjectEvidence(ctx, root, path); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
				changed[path] = true
				dirs[filepath.Dir(path)] = true
			}
			m, err := store.RefreshProject(ctx, root)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			files := make([]selectedFile, 0)
			related := map[string]bool{}
			sources, _, err := graphSources(ctx, root, store)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			graph, err := codegraph.BuildGo(ctx, root, sources)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			nodePaths := make(map[string]string, len(graph.Nodes))
			for _, node := range graph.Nodes {
				nodePaths[node.ID] = node.Path
			}
			for _, edge := range graph.Edges {
				from, to := nodePaths[edge.From], nodePaths[edge.To]
				if from == "" || to == "" {
					continue
				}
				if changed[from] {
					related[to] = true
				}
				if changed[to] {
					related[from] = true
				}
			}
			words := strings.Fields(strings.ToLower(p.Query))
			for _, file := range m.Files {
				if name == "test_select" && !file.Test {
					continue
				}
				score, reason := 0, ""
				if changed[file.Path] {
					score, reason = 100, "explicit changed file"
				} else if dirs[filepath.Dir(file.Path)] {
					score, reason = 50, "same directory as changed source"
				}
				if related[file.Path] && !changed[file.Path] {
					score, reason = 75, "source graph dependency of changed code"
				}
				haystack := strings.ToLower(file.Path + " " + file.Package + " " + strings.Join(file.Imports, " "))
				for _, word := range words {
					if strings.Contains(haystack, word) {
						score += 10
						if reason == "" {
							reason = "query matches path, package or imports"
						}
					}
				}
				if len(changed) == 0 && len(words) == 0 && file.Entry {
					score, reason = 20, "project entry point"
				}
				if score > 0 {
					files = append(files, selectedFile{Path: file.Path, Fingerprint: file.Fingerprint, Score: score, Reason: reason})
				}
			}
			slices.SortFunc(files, func(a, b selectedFile) int {
				if a.Score != b.Score {
					return b.Score - a.Score
				}
				return strings.Compare(a.Path, b.Path)
			})
			total := len(files)
			files = files[:min(len(files), 50)]
			used := 0
			if name == "context_select" {
				selected := make([]selectedFile, 0)
				for _, file := range files {
					data, err := engineering.ReadProjectEvidence(ctx, root, file.Path)
					if err != nil {
						return fantasy.NewTextErrorResponse(err.Error()), nil
					}
					if engineering.Hash(string(data)) != file.Fingerprint {
						return fantasy.NewTextErrorResponse("source changed during selection"), nil
					}
					remaining := 2*(p.TokenBudget-used) - 128 - len(file.Path) - len(file.Reason)
					if remaining <= 0 {
						break
					}
					content := string(data)
					if len(content) > remaining {
						content = validUTF8Prefix(content, remaining)
						file.Truncated = true
					}
					file.Content = content
					used += (len(content) + len(file.Path) + len(file.Reason) + 129) / 2
					selected = append(selected, file)
				}
				files = selected
			}
			result := map[string]any{"files": files, "total_candidates": total, "map_truncated": m.Truncated, "partial": true, "basis": "Go source graph dependencies, path proximity and query matches; runtime dependencies are not inferred", "graph_gaps": graph.Gaps}
			if name == "test_select" {
				result["full_suite_required"] = true
			} else {
				result["estimated_tokens"] = used
				result["token_budget"] = p.TokenBudget
				result["tokenizer"] = "conservative UTF-8 byte estimate"
			}
			data, err := json.Marshal(result)
			return fantasy.NewTextResponse(string(data)), err
		}))
	}
	return append(tools, fantasy.NewAgentTool("contract_diff", selectionDescription, func(ctx context.Context, p struct {
		BeforePath string `json:"before_path"`
		AfterPath  string `json:"after_path"`
	}, call fantasy.ToolCall,
	) (fantasy.ToolResponse, error) {
		before, err := engineering.ReadProjectEvidence(ctx, root, p.BeforePath)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		after, err := engineering.ReadProjectEvidence(ctx, root, p.AfterPath)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		comparison, err := experiments.CompareOpenAPI(ctx, before, after)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		data, err := json.Marshal(comparison)
		return fantasy.NewTextResponse(string(data)), err
	}))
}

func validUTF8Prefix(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && text[limit]&0xc0 == 0x80 {
		limit--
	}
	return text[:limit]
}
