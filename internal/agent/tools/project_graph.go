package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

func graphSources(ctx context.Context, root string, store *engineering.Store) ([]codegraph.Source, bool, error) {
	project, err := store.RefreshProject(ctx, root)
	if err != nil {
		return nil, false, err
	}
	sources := make([]codegraph.Source, 0, len(project.Files))
	for _, file := range project.Files {
		data, err := engineering.ReadProjectEvidence(ctx, root, file.Path)
		if err != nil {
			return nil, false, err
		}
		if engineering.Hash(string(data)) != file.Fingerprint {
			return nil, false, fmt.Errorf("source changed while reading graph")
		}
		sources = append(sources, codegraph.Source{Path: file.Path, Hash: file.Fingerprint, Content: data})
	}
	return sources, project.Truncated, nil
}

func projectGraph(ctx context.Context, root string, store *engineering.Store, params ProjectMapParams, providers ...LSPGraphProvider) (fantasy.ToolResponse, error) {
	namespace, err := codegraph.Namespace(root)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	key := "graph"
	if params.FilePath != "" {
		if !graphLanguageFile(params.FilePath) {
			return fantasy.NewTextErrorResponse("LSP scope must be a supported source file"), nil
		}
		if _, err := engineering.ReadProjectEvidence(ctx, root, params.FilePath); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		key += "-" + engineering.Hash(filepath.ToSlash(filepath.Clean(params.FilePath)))
	}
	respond := func(value any) (fantasy.ToolResponse, error) {
		data, err := json.Marshal(value)
		return fantasy.NewTextResponse(string(data)), err
	}
	sources, partial, err := graphSources(ctx, root, store)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if params.Action == "refresh_graph" {
		var graph codegraph.CodeGraph
		if params.FilePath == "" {
			graph, err = codegraph.BuildGo(ctx, root, sources)
		} else if len(providers) == 0 || providers[0] == nil {
			return fantasy.NewTextErrorResponse("scoped LSP graph provider is unavailable"), nil
		} else {
			graph, err = providers[0].GraphForFile(ctx, filepath.Join(root, params.FilePath))
			graph.SourceFingerprint = codegraph.Fingerprint(sources)
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		if partial {
			graph.Partial = true
			graph.Gaps = append(graph.Gaps, "Project map truncated; omitted files are not proven deleted")
		}
		if err := validateGraphNodes(ctx, root, graph); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		graph.Gaps = append(graph.Gaps, "Analysis is source-scoped; runtime data flow is not analyzed")
		old, _, readErr := store.ReadRecord(ctx, namespace, key)
		if readErr != nil {
			old = engineering.Record{}
		}
		data, err := json.Marshal(graph)
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if _, err := store.PutRecord(ctx, namespace, key, old.Revision, data); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		return respond(graph)
	}
	_, data, err := store.ReadRecord(ctx, namespace, key)
	if err != nil {
		return fantasy.NewTextErrorResponse("Refresh the code graph first: " + err.Error()), nil
	}
	var graph codegraph.CodeGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if graph.SourceFingerprint != codegraph.Fingerprint(sources) || graph.Root != root {
		return fantasy.NewTextErrorResponse("source changed; refresh_graph before querying"), nil
	}
	if err := validateGraphNodes(ctx, root, graph); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	var page codegraph.Page
	query := codegraph.Query{Text: params.Query, Cursor: params.Cursor, Depth: params.Depth}
	if params.Action == "symbols" {
		page, err = codegraph.Symbols(ctx, graph, query)
	} else {
		page, err = codegraph.Impact(ctx, graph, params.NodeID, query)
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	return respond(page)
}

func graphLanguageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".cs", ".swift", ".kt", ".vue", ".svelte":
		return true
	}
	return false
}

func validateGraphNodes(ctx context.Context, root string, graph codegraph.CodeGraph) error {
	if graph.Root != root || len(graph.Nodes) > 20000 || len(graph.Edges) > 50000 {
		return fmt.Errorf("invalid graph root or size")
	}
	seen := map[string]string{}
	for _, node := range graph.Nodes {
		if !graphLanguageFile(node.Path) {
			return fmt.Errorf("graph contains an unsupported source path")
		}
		if old, ok := seen[node.Path]; ok {
			if old != node.FileHash {
				return fmt.Errorf("inconsistent graph source hashes")
			}
			continue
		}
		data, err := engineering.ReadProjectEvidence(ctx, root, node.Path)
		if err != nil {
			return err
		}
		if engineering.Hash(string(data)) != node.FileHash {
			return fmt.Errorf("graph source changed: %s", node.Path)
		}
		seen[node.Path] = node.FileHash
	}
	return nil
}
