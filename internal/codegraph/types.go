// Package codegraph builds bounded, source-grounded code relationships.
package codegraph

type Source struct {
	Path    string
	Hash    string
	Content []byte
}

type Node struct {
	ID        string `json:"id"`
	Language  string `json:"language"`
	Path      string `json:"path"`
	Symbol    string `json:"symbol"`
	Kind      string `json:"kind"`
	FileHash  string `json:"file_hash"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	Origin     string `json:"origin"`
}

type CodeGraph struct {
	Root              string   `json:"root"`
	SourceFingerprint string   `json:"source_fingerprint"`
	Nodes             []Node   `json:"nodes"`
	Edges             []Edge   `json:"edges"`
	Partial           bool     `json:"partial"`
	Gaps              []string `json:"gaps"`
}

type Query struct {
	Text   string
	Cursor string
	Depth  int
}

type Page struct {
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
	Cursor  string   `json:"cursor,omitempty"`
	Partial bool     `json:"partial"`
	Gaps    []string `json:"gaps"`
}
