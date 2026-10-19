package codemap

import (
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Schemas and infrastructure: the tables, views and functions of SQL and
// the queries that use them; the types of GraphQL, Protobuf, Thrift and
// Prisma; the endpoints and schemas of an OpenAPI document; the resources,
// modules and variables of Terraform.

// tokenNames are helpers shared by the schema readers.
type tokenReader struct {
	toks  []lexToken
	match map[int]int
	lines []string
	out   *File
	names map[int]bool
}

func newTokenReader(out *File, content, lang string) *tokenReader {
	toks := lex(content, languages[lang])
	return &tokenReader{toks: toks, match: matchBrackets(toks), lines: strings.Split(content, "\n"), out: out, names: map[int]bool{}}
}

func (r *tokenReader) tok(i int) lexToken {
	if i < 0 || i >= len(r.toks) {
		return lexToken{kind: tPunct}
	}
	return r.toks[i]
}

