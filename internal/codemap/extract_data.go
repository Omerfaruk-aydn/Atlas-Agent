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

func extractSQL(out *File, content string) {
	r := newTokenReader(out, content, "sql")
	for i := 0; i < len(r.toks); i++ {
		t := r.toks[i]
		if t.kind != tIdent || r.names[i] {
			continue
		}
		switch w := r.lower(i); w {
		case "create":
			k := i + 1
			for sqlModifiers[r.lower(k)] || r.tok(k+1).text == "=" && r.tok(k).kind == tIdent {
				if r.tok(k+1).text == "=" {
					k += 2
				}
				k++
			}
			obj := r.lower(k)
			if !sqlObjects[obj] {
				continue
			}
			k++
			if r.lower(k) == "concurrently" {
				k++
			}
			if r.lower(k) == "if" {
				for r.lower(k) == "if" || r.lower(k) == "not" || r.lower(k) == "exists" {
					k++
				}
			}
			name, after := r.sqlName(k)
			if name == "" {
				continue
			}
			// The last part of schema.name is the name.
			at := after - 1
			end := r.statementEnd(after)
			r.define(at, strings.Trim(r.toks[at].text, "\"`[]"), obj, "", end)
			if obj == "index" || obj == "trigger" || obj == "policy" {
				for j := after; j < end; j++ {
					if r.lower(j) == "on" {
						if table, _ := r.sqlName(j + 1); table != "" {
							r.ref(table, "", RefTable, r.toks[j].line)
						}
						break
					}
				}
			}
			i = after - 1
		case "alter", "drop", "truncate", "lock", "analyze", "vacuum":
			k := i + 1
			if r.lower(k) != "table" && w != "truncate" && w != "lock" {
				continue
			}
			if r.lower(k) == "table" {
				k++
			}
			for r.lower(k) == "if" || r.lower(k) == "exists" || r.lower(k) == "only" {
				k++
			}
			if name, after := r.sqlName(k); name != "" {
				r.ref(name, "", RefTable, t.line)
				i = after - 1
			}
		case "from", "join", "into", "update", "references", "call", "exec", "execute", "perform":
			if w == "update" && r.lower(i+1) == "set" {
				continue
			}
			k := i + 1
			if r.lower(k) == "only" || r.lower(k) == "lateral" {
				k++
			}
			for {
				name, after := r.sqlName(k)
				if name == "" || sqlNotName[name] {
					break
				}
				r.ref(name, "", RefTable, r.toks[k].line)
				k = after
				if r.lower(k) == "as" {
					k++
				}
				if r.tok(k).kind == tIdent && !sqlKeyword(r.lower(k)) {
					// An alias.
					r.names[k] = true
					k++
				}
				if r.tok(k).text != "," || w == "into" || w == "update" {
					break
				}
				k++
			}
			i = k - 1
		default:
			if r.tok(i+1).text == "(" && r.tok(i-1).text != "." && !sqlNotCalls[w] && !sqlKeyword(w) && !sqlBuiltins[w] {
				r.ref(w, "", RefTable, t.line)
			}
		}
	}
	// Named queries, as sqlc and similar tools write them: -- name: GetUser :one.
	var queries []int
	for n, line := range r.lines {
		if m := sqlQueryRe.FindStringSubmatch(line); m != nil {
			queries = append(queries, len(out.Symbols))
			out.Symbols = append(out.Symbols, Symbol{Name: m[1], Kind: "query", Line: n + 1, EndLine: n + 1, Signature: clipSignature(line), Exported: true})
		}
	}
	for k, s := range queries {
		end := out.Lines
		if k+1 < len(queries) {
			end = out.Symbols[queries[k+1]].Line - 1
		}
		for end > out.Symbols[s].Line && strings.TrimSpace(r.lines[end-1]) == "" {
			end--
		}
		out.Symbols[s].EndLine = end
	}
}

var sqlKeywords = words("select from where group order by having limit offset join left right inner outer full cross on and or not as set values returning with union all distinct case when then else end is null in like between exists insert update delete into create table primary key foreign references default check unique index natural using window partition over asc desc nulls first last fetch next rows only")

func sqlKeyword(w string) bool { return sqlKeywords[w] }

var graphqlScalars = words("String Int Float Boolean ID")

func extractGraphQL(out *File, content string) {
	r := newTokenReader(out, content, "graphql")
	kinds := map[string]string{"type": "type", "interface": "interface", "enum": "enum", "input": "input", "union": "union", "scalar": "scalar"}
	roots := words("Query Mutation Subscription")
	depth := 0
	current := ""
	for i := 0; i < len(r.toks); i++ {
		t := r.toks[i]
		switch t.text {
		case "{":
			depth++
			continue
		case "}":
			depth--
			if depth == 0 {
				current = ""
			}
			continue
		}
		if t.kind != tIdent || r.names[i] {
			continue
		}
		if depth == 0 {
			body := r.bodyAfter(i)
			switch {
			case t.text == "extend" && kinds[r.tok(i+1).text] != "":
				if n := r.tok(i + 2); n.kind == tIdent {
					r.names[i+2] = true
					r.ref(n.text, "", RefInherit, n.line)
					current = n.text
				}
				i++
			case kinds[t.text] != "" && r.tok(i+1).kind == tIdent:
				r.define(i+1, r.toks[i+1].text, kinds[t.text], "", body)
				current = r.toks[i+1].text
				i++
			case (t.text == "query" || t.text == "mutation" || t.text == "subscription") && r.tok(i+1).kind == tIdent:
				r.define(i+1, r.toks[i+1].text, "operation", "", body)
				i++
			case t.text == "fragment" && r.tok(i+1).kind == tIdent:
				r.define(i+1, r.toks[i+1].text, "fragment", "", body)
				i++
			case t.text == "directive" && r.tok(i+1).text == "@" && r.tok(i+2).kind == tIdent:
				r.define(i+2, "@"+r.toks[i+2].text, "directive", "", -1)
				i += 2
			case isUpperStart(t.text) && !graphqlScalars[t.text]:
				// implements A & B, union U = A | B, on Type, schema roots.
				r.ref(t.text, "", RefUse, t.line)
			}
			continue
		}
		prev := r.tok(i - 1).text
		switch {
		case prev == "...":
			if t.text != "on" {
				r.ref(t.text, "", RefUse, t.line)
			}
		case depth == 1 && roots[current] && t.first && (r.tok(i+1).text == ":" || r.tok(i+1).text == "("):
			end := i
			for end+1 < len(r.toks) && r.toks[end+1].line == t.line {
				end++
			}
			r.define(i, t.text, "field", current, end)
		case isUpperStart(t.text) && !graphqlScalars[t.text] && (prev == ":" || prev == "[" || prev == "on" || prev == "!" || prev == "|" || prev == "&"):
			r.ref(t.text, "", RefUse, t.line)
		}
	}
}

// bodyAfter returns the } closing the body of the definition starting at
// i, or -1.
func (r *tokenReader) bodyAfter(i int) int {
	for j := i + 1; j < len(r.toks) && j < i+200; j++ {
		switch r.toks[j].text {
		case "{":
			if e, ok := r.match[j]; ok {
				return e
			}
			return -1
		case "}":
			return -1
		}
		if r.toks[j].first && r.toks[j].kind == tIdent && r.toks[j].indent == 0 && j > i+1 {
			if _, ok := map[string]bool{"type": true, "interface": true, "enum": true, "input": true, "union": true, "scalar": true, "extend": true, "query": true, "mutation": true, "subscription": true, "fragment": true, "schema": true, "directive": true}[r.toks[j].text]; ok {
				return j - 1
			}
		}
	}
	return -1
}

var idlScalars = words("double float int32 int64 uint32 uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool string bytes i8 i16 i32 i64 byte binary void list set map optional required repeated stream returns oneof reserved extensions option syntax package import public weak to max")

// extractIDL reads Protobuf and Thrift: messages, structs, enums,
// services and their methods, and the types their fields use.
func extractIDL(out *File, content string) {
	r := newTokenReader(out, content, out.Language)
	kinds := map[string]string{
		"message": "struct", "enum": "enum", "service": "interface", "struct": "struct", "union": "struct",
		"exception": "struct", "typedef": "type", "senum": "enum", "extend": "",
	}
	var stack []string
	var ends []int
	for i := 0; i < len(r.toks); i++ {
		for len(ends) > 0 && i > ends[len(ends)-1] {
			stack, ends = stack[:len(stack)-1], ends[:len(ends)-1]
		}
		t := r.toks[i]
		if t.kind != tIdent || r.names[i] {
			continue
		}
		container := ""
		if len(stack) > 0 {
			container = stack[len(stack)-1]
		}
		if (t.text == "import" || t.text == "include") && r.tok(i+1).kind == tString || t.text == "import" && r.tok(i+2).kind == tString {
			spec := r.tok(i + 1)
			if spec.kind != tString {
				spec = r.tok(i + 2)
			}
			out.Imports = append(out.Imports, Import{Spec: spec.text, Line: t.line})
			continue
		}
		if kind, ok := kinds[t.text]; ok && r.tok(i+1).kind == tIdent {
			name := r.tok(i + 1)
			if t.text == "typedef" {
				// typedef Type Name
				r.ref(name.text, "", RefUse, name.line)
				if n := r.tok(i + 2); n.kind == tIdent {
					r.define(i+2, n.text, "type", container, -1)
				}
				i += 2
				continue
			}
			if kind == "" {
				r.names[i+1] = true
				r.ref(name.text, "", RefInherit, name.line)
				i++
				continue
			}
			body := r.bodyAfter(i)
			r.define(i+1, name.text, kind, container, body)
			if body > 0 {
				stack, ends = append(stack, name.text), append(ends, body)
			}
			i++
			continue
		}
		if t.text == "rpc" && r.tok(i+1).kind == tIdent {
			end := r.statementEnd(i)
			if e := r.bodyAfter(i); e > 0 && e < end {
				end = e
			}
			r.define(i+1, r.toks[i+1].text, "method", container, end)
			i++
			continue
		}
		if idlScalars[t.text] || t.text == "rpc" || t.text == "const" {
			continue
		}
		// A type in a field or a method: Type name, or a.b.Type.
		k := i
		name := t.text
		for r.tok(k+1).text == "." && r.tok(k+2).kind == tIdent {
			k += 2
			name = r.toks[k].text
		}
		next := r.tok(k + 1)
		prev := r.tok(i - 1).text
		if prev == "." {
			continue
		}
		if next.kind == tIdent || next.text == ">" || prev == "(" || prev == "<" || prev == "," && r.tok(k+1).text != "=" {
			if !idlScalars[name] && isUpperStart(name) {
				r.ref(name, "", RefUse, t.line)
			}
		}
		if out.Language == "thrift" && next.kind == tIdent && r.tok(k+2).text == "(" && len(stack) > 0 {
			// RetType method(...) in a service.
			end := r.match[k+2]
			r.define(k+1, next.text, "method", container, end)
		}
		i = k
	}
}

var prismaScalars = words("String Int BigInt Float Decimal Boolean DateTime Json Bytes Unsupported")

// extractPrisma reads models, enums, types and views and the relations
// their fields declare.
func extractPrisma(out *File, content string) {
	r := newTokenReader(out, content, "prisma")
	kinds := map[string]string{"model": "model", "enum": "enum", "type": "type", "view": "model"}
	for i := 0; i < len(r.toks); i++ {
		t := r.toks[i]
		if t.kind != tIdent || !t.first {
			continue
		}
		if kind := kinds[t.text]; kind != "" && t.indent == 0 && r.tok(i+1).kind == tIdent {
			r.define(i+1, r.toks[i+1].text, kind, "", r.bodyAfter(i))
			i++
			continue
		}
		if n := r.tok(i + 1); t.indent > 0 && n.kind == tIdent && n.line == t.line && isUpperStart(n.text) && !prismaScalars[n.text] {
			r.ref(n.text, "", RefUse, n.line)
		}
	}
}

// extractOpenAPI reads an OpenAPI or Swagger document: each endpoint, each
// schema, and the $refs between them.
func extractOpenAPI(out *File, content string) {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(content), &doc) != nil || len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0]
	lines := strings.Split(content, "\n")
	sig := func(line int) string {
		if line < 1 || line > len(lines) {
			return ""
		}
		return clipSignature(lines[line-1])
	}
	methods := words("get put post delete patch options head trace")
	if paths := yamlGet(root, "paths"); paths != nil {
		for k := 0; k+1 < len(paths.Content); k += 2 {
			route, ops := paths.Content[k], paths.Content[k+1]
			for m := 0; m+1 < len(ops.Content); m += 2 {
				method := ops.Content[m]
				if !methods[strings.ToLower(method.Value)] {
					continue
				}
				summary := ""
				if s := yamlGet(ops.Content[m+1], "summary"); s != nil {
					summary = " — " + s.Value
				} else if s := yamlGet(ops.Content[m+1], "operationId"); s != nil {
					summary = " — " + s.Value
				}
				out.Symbols = append(out.Symbols, Symbol{
					Name: strings.ToUpper(method.Value) + " " + route.Value, Kind: "endpoint", Line: method.Line,
					EndLine: yamlEnd(ops.Content[m+1]), Signature: clipSignature(strings.ToUpper(method.Value) + " " + route.Value + summary), Exported: true,
				})
			}
		}
	}
	schemas := yamlGet(yamlGet(root, "components"), "schemas")
	if schemas == nil {
		schemas = yamlGet(root, "definitions")
	}
	if schemas != nil {
		for k := 0; k+1 < len(schemas.Content); k += 2 {
			key := schemas.Content[k]
			out.Symbols = append(out.Symbols, Symbol{Name: key.Value, Kind: "schema", Line: key.Line, EndLine: yamlEnd(schemas.Content[k+1]), Signature: sig(key.Line), Exported: true})
		}
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.MappingNode {
			for k := 0; k+1 < len(n.Content); k += 2 {
				if n.Content[k].Value == "$ref" && n.Content[k+1].Kind == yaml.ScalarNode {
					openAPIRef(out, n.Content[k+1].Value, n.Content[k+1].Line)
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
}

func openAPIRef(out *File, ref string, line int) {
	file, pointer, _ := strings.Cut(ref, "#")
	if file != "" && !hasScheme(file) {
		out.Imports = append(out.Imports, Import{Spec: file, Line: line})
	}
	if pointer != "" {
		addRef(out, Ref{Name: path.Base(pointer), Kind: RefUse, Line: line})
	}
}

// yamlGet returns the value of key in a mapping node.
func yamlGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for k := 0; k+1 < len(n.Content); k += 2 {
		if n.Content[k].Value == key {
			return n.Content[k+1]
		}
	}
	return nil
}

// yamlEnd returns the last line a node spans.
func yamlEnd(n *yaml.Node) int {
	end := n.Line
	for _, c := range n.Content {
		end = max(end, yamlEnd(c))
	}
	return end
}

var (
	tfRefRe       = regexp.MustCompile(`\b(var|local|module)\.([A-Za-z_][\w-]*)`)
	tfDataRefRe   = regexp.MustCompile(`\bdata\.([A-Za-z_][\w-]*)\.([A-Za-z_][\w-]*)`)
	tfResourceRe  = regexp.MustCompile(`\b([a-z][a-z0-9]*_[a-z0-9_]+)\.([A-Za-z_][\w-]*)`)
	tfNotResource = words("path each count terraform self")
)

// extractTerraform reads resources, data sources, modules, variables,
// outputs and locals, and the references between them.
func extractTerraform(out *File, content string) {
	r := newTokenReader(out, content, "terraform")
	depth := 0
	for i := 0; i < len(r.toks); i++ {
		t := r.toks[i]
		switch t.text {
		case "{":
			depth++
			continue
		case "}":
			depth--
			continue
		}
		if t.kind == tString {
			tfStringRefs(out, t.text, t.line)
			continue
		}
		if t.kind != tIdent {
			continue
		}
		if depth == 0 && t.first {
			label := func(k int) string {
				if s := r.tok(k); s.kind == tString || s.kind == tIdent {
					return s.text
				}
				return ""
			}
			end := r.bodyAfterBrace(i)
			switch t.text {
			case "resource", "data":
				typ, name := label(i+1), label(i+2)
				if typ != "" && name != "" {
					full := typ + "." + name
					if t.text == "data" {
						full = "data." + full
					}
					r.define(i, full, t.text, "", end)
					i += 2
				}
			case "module", "variable", "output":
				if name := label(i + 1); name != "" {
					prefix := map[string]string{"module": "module.", "variable": "var.", "output": "output."}[t.text]
					r.define(i, prefix+name, t.text, "", end)
					if t.text == "module" && end > 0 {
						for k := i + 2; k < end; k++ {
							if r.toks[k].text == "source" && r.tok(k+1).text == "=" && r.tok(k+2).kind == tString {
								if src := r.toks[k+2].text; strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") {
									out.Imports = append(out.Imports, Import{Spec: src, Line: r.toks[k+2].line})
								}
							}
						}
					}
					i++
				}
			case "locals":
				for k := i + 1; k < end; k++ {
					if s := r.toks[k]; s.first && s.kind == tIdent && r.tok(k+1).text == "=" {
						r.define(k, "local."+s.text, "local", "", k)
					}
				}
			}
			continue
		}
		if r.tok(i+1).text != "." || r.tok(i+2).kind != tIdent || r.tok(i-1).text == "." {
			continue
		}
		second := r.toks[i+2].text
		switch {
		case t.text == "var" || t.text == "local" || t.text == "module":
			r.ref(t.text+"."+second, "", RefInfra, t.line)
		case t.text == "data" && r.tok(i+3).text == "." && r.tok(i+4).kind == tIdent:
			r.ref("data."+second+"."+r.toks[i+4].text, "", RefInfra, t.line)
		case strings.Contains(t.text, "_") && !tfNotResource[t.text]:
			r.ref(t.text+"."+second, "", RefInfra, t.line)
		}
	}
}

// bodyAfterBrace returns the } closing the first { after i.
func (r *tokenReader) bodyAfterBrace(i int) int {
	for j := i + 1; j < len(r.toks) && j < i+12; j++ {
		if r.toks[j].text == "{" {
			if e, ok := r.match[j]; ok {
				return e
			}
		}
	}
	return -1
}

// tfStringRefs records the references inside "${...}" of a string.
func tfStringRefs(out *File, s string, line int) {
	if !strings.Contains(s, "${") {
		return
	}
	for _, m := range tfRefRe.FindAllStringSubmatch(s, -1) {
		addRef(out, Ref{Name: m[1] + "." + m[2], Kind: RefInfra, Line: line})
	}
	for _, m := range tfDataRefRe.FindAllStringSubmatch(s, -1) {
		addRef(out, Ref{Name: "data." + m[1] + "." + m[2], Kind: RefInfra, Line: line})
	}
	for _, m := range tfResourceRe.FindAllStringSubmatch(s, -1) {
		if m[1] != "data" && !tfNotResource[m[1]] {
			addRef(out, Ref{Name: m[1] + "." + m[2], Kind: RefInfra, Line: line})
		}
	}
}
