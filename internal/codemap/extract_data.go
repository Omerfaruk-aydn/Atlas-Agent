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

// lower returns the token at i in lower case.
func (r *tokenReader) lower(i int) string {
	return strings.ToLower(r.tok(i).text)
}

func (r *tokenReader) signature(line int) string {
	if line < 1 || line > len(r.lines) {
		return ""
	}
	return clipSignature(r.lines[line-1])
}

// define records a symbol named at token i that ends at token end.
func (r *tokenReader) define(i int, name, kind, container string, end int) int {
	if len(r.out.Symbols) >= maxSymbolsPerFile {
		return -1
	}
	line := r.tok(i).line
	endLine := line
	if end >= 0 && end < len(r.toks) {
		endLine = r.toks[end].line
	}
	r.names[i] = true
	r.out.Symbols = append(r.out.Symbols, Symbol{Name: name, Kind: kind, Container: container, Line: line, EndLine: max(line, endLine), Signature: r.signature(line), Exported: true})
	return len(r.out.Symbols) - 1
}

func (r *tokenReader) ref(name, qualifier, kind string, line int) {
	addRef(r.out, Ref{Name: name, Qualifier: qualifier, Kind: kind, Line: line})
}

// statementEnd returns the index of the ; ending the statement at k,
// skipping bracketed parts.
func (r *tokenReader) statementEnd(k int) int {
	for j := k; j < len(r.toks); j++ {
		switch r.toks[j].text {
		case ";":
			return j
		case "(", "[", "{":
			if e, ok := r.match[j]; ok {
				j = e
			}
		}
	}
	return len(r.toks) - 1
}

var (
	sqlObjects   = words("table view function procedure trigger index type schema sequence domain policy")
	sqlModifiers = words("or replace temp temporary unlogged global local materialized unique virtual external recursive constraint definer security algorithm sql")
	sqlNotCalls  = words("values in exists any all key check unique primary foreign default not null references as on using over filter within array row cast table index returns returning into select")
	// sqlBuiltins are functions every database provides; a call of one is
	// not a use of the project's own.
	sqlBuiltins = words("count sum avg min max coalesce nullif greatest least now lower upper length char_length substring substr " +
		"concat concat_ws trim ltrim rtrim replace round floor ceil ceiling abs power sqrt mod random extract date_trunc date_part " +
		"to_char to_date to_timestamp to_number age interval current_date current_timestamp localtimestamp position strpos " +
		"json_build_object jsonb_build_object json_agg jsonb_agg array_agg string_agg row_number rank dense_rank lag lead " +
		"first_value last_value ntile nextval currval setval gen_random_uuid uuid_generate_v4 ifnull isnull iif datediff " +
		"dateadd getdate strftime julianday datetime date time format left right split_part regexp_replace unnest generate_series")
)

// sqlName reads a possibly quoted, possibly schema-qualified name at k and
// returns it in lower case with the index after it.
func (r *tokenReader) sqlName(k int) (string, int) {
	name := ""
	for {
		t := r.tok(k)
		if t.kind != tIdent && t.kind != tString {
			break
		}
		name = t.text
		r.names[k] = true
		k++
		if r.tok(k).text != "." {
			break
		}
		k++
	}
	return sqlIdent(name), k
}

