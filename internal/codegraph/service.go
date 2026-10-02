package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func digest(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func Namespace(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return "codegraph-" + digest(root), nil
}

func Fingerprint(sources []Source) string {
	entries := make([]string, len(sources))
	for i, source := range sources {
		entries[i] = source.Path + "\x00" + source.Hash
	}
	sort.Strings(entries)
	return digest(strings.Join(entries, "\x00"))
}

func Symbols(ctx context.Context, graph CodeGraph, query Query) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if len(query.Text) > 256 || len(query.Cursor) > 256 {
		return Page{}, fmt.Errorf("graph query exceeds size limit")
	}
	encoded, _ := json.Marshal(graph)
	identity := digest(string(encoded) + "\x00" + query.Text)
	offset := 0
	if query.Cursor != "" {
		parts := strings.Split(query.Cursor, ":")
		if len(parts) != 2 || parts[0] != identity {
			return Page{}, fmt.Errorf("stale or invalid graph cursor")
		}
		var err error
		offset, err = strconv.Atoi(parts[1])
		if err != nil || offset < 0 {
			return Page{}, fmt.Errorf("invalid graph cursor offset")
		}
	}
	var matching []Node
	for _, node := range graph.Nodes {
		if strings.Contains(strings.ToLower(node.Path+" "+node.Symbol+" "+node.Kind), strings.ToLower(query.Text)) {
			matching = append(matching, node)
		}
	}
	if offset > len(matching) {
		return Page{}, fmt.Errorf("graph cursor out of range")
	}
	end := min(offset+50, len(matching))
	page := Page{Nodes: matching[offset:end], Edges: []Edge{}, Partial: graph.Partial, Gaps: graph.Gaps}
	if end < len(matching) {
		page.Cursor = identity + ":" + strconv.Itoa(end)
	}
	return page, nil
}

func Impact(ctx context.Context, graph CodeGraph, id string, query Query) (Page, error) {
	if query.Depth == 0 {
		query.Depth = 2
	}
	if query.Depth < 1 || query.Depth > 5 || query.Cursor != "" {
		return Page{}, fmt.Errorf("impact depth must be 1-5; cursors are for symbols")
	}
	nodes := map[string]Node{}
	for _, node := range graph.Nodes {
		nodes[node.ID] = node
	}
	if _, ok := nodes[id]; !ok {
		return Page{}, fmt.Errorf("unknown graph node")
	}
	page := Page{Nodes: []Node{nodes[id]}, Edges: []Edge{}, Partial: graph.Partial, Gaps: append([]string(nil), graph.Gaps...)}
	seen := map[string]bool{id: true}
	frontier := map[string]bool{id: true}
	for range query.Depth {
		if err := ctx.Err(); err != nil {
			return Page{}, err
		}
		next := map[string]bool{}
		for _, edge := range graph.Edges {
			if !frontier[edge.To] {
				continue
			}
			if _, ok := nodes[edge.From]; !ok {
				continue
			}
			if len(page.Edges) >= 200 || !seen[edge.From] && len(page.Nodes) >= 200 {
				page.Partial = true
				page.Gaps = append(page.Gaps, "Impact traversal reached 200 nodes/edges")
				return page, nil
			}
			page.Edges = append(page.Edges, edge)
			if !seen[edge.From] {
				seen[edge.From] = true
				next[edge.From] = true
				page.Nodes = append(page.Nodes, nodes[edge.From])
			}
		}
		frontier = next
	}
	return page, nil
}
