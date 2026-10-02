package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/codegraph"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-powernap/pkg/lsp/protocol"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lsp"
)

type LSPGraphProvider interface {
	GraphForFile(ctx context.Context, path string) (codegraph.CodeGraph, error)
}

type lspGraphProvider struct {
	root    string
	manager *lsp.Manager
}

func NewLSPGraphProvider(root string, manager *lsp.Manager) LSPGraphProvider {
	return lspGraphProvider{root: root, manager: manager}
}

func (p lspGraphProvider) GraphForFile(ctx context.Context, path string) (codegraph.CodeGraph, error) {
	graph := codegraph.CodeGraph{Root: p.root, Nodes: []codegraph.Node{}, Edges: []codegraph.Edge{}, Partial: true, Gaps: []string{"Scoped LSP result; omitted files and dynamic calls are not certified"}}
	if p.manager == nil {
		return graph, fmt.Errorf("LSP manager unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p.manager.Start(ctx, path)
	client := findLSPClient(p.manager, path)
	if client == nil {
		return graph, fmt.Errorf("no LSP client for this file")
	}
	symbols, err := client.DocumentSymbols(ctx, path)
	if err != nil {
		return graph, err
	}
	seen := map[string]bool{}
	add := func(file, name string, rng protocol.Range) (string, error) {
		rel, err := filepath.Rel(p.root, file)
		if err != nil || !graphLanguageFile(rel) {
			return "", fmt.Errorf("LSP reference outside supported source scope")
		}
		data, err := engineering.ReadProjectEvidence(ctx, p.root, rel)
		if err != nil {
			return "", err
		}
		if len(data) > 64*1024 {
			return "", fmt.Errorf("LSP source exceeds 64 KiB")
		}
		id := engineering.Hash(filepath.ToSlash(rel) + "\x00" + name + "\x00" + strconv.Itoa(int(rng.Start.Line)))
		if !seen[id] {
			if len(graph.Nodes) >= 200 {
				return "", fmt.Errorf("LSP scope reached 200 nodes")
			}
			graph.Nodes = append(graph.Nodes, codegraph.Node{ID: id, Language: filepath.Ext(rel), Path: filepath.ToSlash(rel), Symbol: name, Kind: "lsp-symbol", FileHash: engineering.Hash(string(data)), StartLine: int(rng.Start.Line) + 1, EndLine: int(rng.End.Line) + 1})
			seen[id] = true
		}
		return id, nil
	}
	var visit func(protocol.DocumentSymbolResult)
	queries := 0
	visit = func(symbol protocol.DocumentSymbolResult) {
		if ctx.Err() != nil {
			return
		}
		id, err := add(path, symbol.GetName(), symbol.GetRange())
		if err != nil {
			if len(graph.Gaps) < 32 {
				graph.Gaps = append(graph.Gaps, err.Error())
			}
			return
		}
		rng := symbol.GetRange()
		if ds, ok := symbol.(*protocol.DocumentSymbol); ok {
			rng = ds.SelectionRange
			for i := range ds.Children {
				visit(&ds.Children[i])
			}
		}
		if queries >= 16 {
			return
		}
		queries++
		items, err := client.PrepareCallHierarchy(ctx, path, int(rng.Start.Line)+1, int(rng.Start.Character)+1)
		if err != nil || len(items) == 0 {
			return
		}
		calls, err := client.OutgoingCalls(ctx, items[0])
		if err != nil {
			return
		}
		for _, call := range calls {
			if len(graph.Edges) >= 200 {
				break
			}
			file, err := call.To.URI.Path()
			if err != nil {
				continue
			}
			to, err := add(file, call.To.Name, call.To.Range)
			if err != nil {
				continue
			}
			graph.Edges = append(graph.Edges, codegraph.Edge{From: id, To: to, Kind: "calls", Resolution: "lsp-reported", Origin: "lsp-call-hierarchy"})
		}
	}
	for _, symbol := range symbols {
		visit(symbol)
	}
	if ctx.Err() != nil {
		return graph, ctx.Err()
	}
	return graph, nil
}
