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

