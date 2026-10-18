package codemap

// File is what one file defines, imports and uses. It depends only on the
// file's content, so it is cached by content hash.
type File struct {
	Path     string   `json:"path"`
	Language string   `json:"language"`
	Hash     string   `json:"hash"`
	Lines    int      `json:"lines"`
	Symbols  []Symbol `json:"symbols,omitempty"`
	Imports  []Import `json:"imports,omitempty"`
	Refs     []Ref    `json:"refs,omitempty"`
	// Links name other files by path: an embedded image, a linked
	// document, a texture a script loads.
	Links []FileLink `json:"links,omitempty"`
	// Asset describes an image, font, media or data file.
	Asset *Asset `json:"asset,omitempty"`
}

// FileLink is a reference to another file by its path.
type FileLink struct {
	// Target is the path as written, with any #fragment; a ${...} in it
	// stands for any name.
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
}

// FileLink kinds, worded to read "a.md embeds b.png".
const (
	LinkEmbeds   = "embeds"
	LinkLinks    = "links to"
	LinkIncludes = "includes"
	LinkLoads    = "loads"
	LinkNames    = "names"
	LinkEntry    = "entry"
	LinkRuns     = "runs"
)

// Asset is what the map measures of a file it does not read.
type Asset struct {
	Bytes  int64  `json:"bytes"`
	Format string `json:"format,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

