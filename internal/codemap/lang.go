// Package codemap builds a map of a project without a parser library: its
// code in every common language, its documentation and its assets. Go is
// read with the standard library's parser; every other language with a
// tokenizer that knows the language's comments and strings, and a table of
// the words that start a definition or an import. Markup, stylesheets,
// schemas, build files and documents have readers of their own, and images
// are measured from their headers.
//
// The map links what each file defines to what uses it: calls across
// languages, a stylesheet's classes to the markup and scripts that use
// them, a table to the queries that read it, a document to the code it
// mentions and the images it embeds. It ranks files by how much the rest of
// the project depends on them, groups them into subsystems, and answers
// where something is defined, who uses it and how two parts connect.
//
// Every link carries how sure it is: resolved through an import or the
// same file, inferred from a name defined once in the project, or
// ambiguous between several definitions. The map is a guide to the
// project, not a replacement for reading it.
package codemap

import (
	"path"
	"strings"
)

// family is how a language marks where a definition ends.
type family int

const (
	// braces ends a definition at its closing brace.
	braces family = iota
	// indent ends a definition where the indentation returns to its level.
	indent
)

// language describes what the tokenizer and the extractor need to know.
type language struct {
	name   string
	family family
	// lineComments start a comment that runs to the end of the line.
	lineComments []string
	// blockComments are start and end markers of comments.
	blockComments [][2]string
	// nestedComments lets block comments nest, as in Rust and Swift.
	nestedComments bool
	// tripleQuotes allows """ and ''' strings.
	tripleQuotes bool
	// backtickStrings makes ` a string delimiter; templates allows ${}
	// inside it.
	backtickStrings, templates bool
	// lifetimes makes a ' followed by a name and no closing quote a
	// lifetime or symbol rather than a character literal.
	lifetimes bool
	// quoteIsPunct makes ' punctuation, as Lisp's quote.
	quoteIsPunct bool
	// longBrackets allows Lua's [[ ]] strings.
	longBrackets bool
	// identExtra are characters besides letters, digits and _ that can
	// appear in a name; identSuffix may end one (Ruby's save!).
	identExtra, identSuffix string
	// decls maps a keyword to the kind of what it defines; the name
	// follows the keyword. A kind ending in ? defines something only when
	// ( follows the name (Groovy's def is also a variable).
	decls map[string]string
	// topLevel are decl keywords that define something only at the start
	// of an unindented line, as OCaml's let; elsewhere they bind locals.
	topLevel map[string]bool
	// declSkip are words between a decl keyword and the name: let rec,
	// type alias.
	declSkip map[string]bool
	// cLike detects definitions without a keyword: name(...) followed by
	// a body, as in C, Java and class methods in JavaScript.
	cLike bool
	// arrows detects name = (...) => and name = function.
	arrows bool
	// notCalls are words followed by ( that are not calls.
	notCalls map[string]bool
	// caseInsensitive languages match keywords in any case.
	caseInsensitive bool
	// endPrefix: a line starting with a word that begins with end closes
	// the definition at its indentation (End Sub, endmodule).
	endPrefix bool
	// continues are words starting a line that still belongs to the
	// definition above at the same indentation (Pascal's var and begin).
	continues map[string]bool
	// clauses: a definition continues on unindented lines starting with
	// its own name (Haskell equations, Erlang clauses).
	clauses bool
	// juxtapose: functions are called without parentheses (f x y), so a
	// name in an expression is a use.
	juxtapose bool
	// lisp: (name args) calls name.
	lisp bool
	// commands: a name at the start of a command is a call (PowerShell).
	commands bool
	// receivers: fn (r T) name declares a method of T.
	receivers bool
	// selfPrefix: member this.Name declares Name (F#).
	selfPrefix bool
}

var (
	cControl = words("if for while switch catch return sizeof typeof alignof decltype new delete throw do else case when foreach using lock fixed synchronized with elif except match not and or in is assert await yield super this self print echo function fn func def lambda")
	jsDecls  = map[string]string{"function": "function", "class": "class", "interface": "interface", "type": "type", "enum": "enum", "namespace": "namespace", "module": "namespace"}
	// mlControl are the keywords of the ML family and Haskell, which call
	// functions without parentheses.
	mlControl = words("let in where case of if then else do module import qualified as hiding data newtype type class instance deriving forall infixl infixr infix family default match with fun function rec and or not begin end open val mutable struct sig functor include exception external method object inherit initializer virtual private new lazy assert when try raise true false type alias exposing port effect namespace member override abstract static inline use yield return elif mod land lor lxor lsl lsr asr")
	// lispControl are special forms, not calls of the project's functions.
	lispControl = words("if when unless cond case let let* letfn loop recur do fn lambda quote and or not def defn defn- defmacro defprotocol defrecord deftype defmulti defmethod definterface defun defclass defstruct defgeneric define define-syntax define-record-type ns require import progn setq setf try catch finally throw doseq dotimes for while begin set! else")
)

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

func merge(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for w := range s {
			out[w] = true
		}
	}
	return out
}

var languages = map[string]*language{
	"typescript": {name: "typescript", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, backtickStrings: true, templates: true, identExtra: "$", decls: jsDecls, cLike: true, arrows: true, notCalls: cControl},
	"javascript": {name: "javascript", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, backtickStrings: true, templates: true, identExtra: "$", decls: jsDecls, cLike: true, arrows: true, notCalls: cControl},
	"python":     {name: "python", family: indent, lineComments: []string{"#"}, tripleQuotes: true, decls: map[string]string{"def": "function", "class": "class"}, notCalls: cControl},
	"mojo":       {name: "mojo", family: indent, lineComments: []string{"#"}, tripleQuotes: true, decls: map[string]string{"def": "function", "fn": "function", "class": "class", "struct": "struct", "trait": "interface"}, notCalls: cControl},
	"java":       {name: "java", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"class": "class", "interface": "interface", "enum": "enum", "record": "class"}, cLike: true, notCalls: cControl},
	"kotlin":     {name: "kotlin", lineComments: []string{"//"}, nestedComments: true, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"fun": "function", "class": "class", "interface": "interface", "object": "class", "typealias": "type"}, notCalls: cControl},
	"csharp":     {name: "csharp", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"class": "class", "interface": "interface", "struct": "struct", "enum": "enum", "record": "class", "namespace": "namespace", "delegate": "type"}, cLike: true, notCalls: cControl},
	"c":          {name: "c", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, decls: map[string]string{"struct": "struct", "enum": "enum", "union": "struct"}, cLike: true, notCalls: cControl},
	"cpp":        {name: "cpp", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, decls: map[string]string{"class": "class", "struct": "struct", "enum": "enum", "union": "struct", "namespace": "namespace"}, cLike: true, notCalls: cControl},
	"glsl":       {name: "glsl", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, decls: map[string]string{"struct": "struct"}, cLike: true, notCalls: cControl},
	"wgsl":       {name: "wgsl", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, nestedComments: true, decls: map[string]string{"fn": "function", "struct": "struct", "alias": "type"}, notCalls: cControl},
	"rust":       {name: "rust", lineComments: []string{"//"}, nestedComments: true, blockComments: [][2]string{{"/*", "*/"}}, lifetimes: true, decls: map[string]string{"fn": "function", "struct": "struct", "enum": "enum", "trait": "interface", "impl": "impl", "mod": "namespace", "type": "type", "macro_rules": "macro"}, notCalls: cControl},
	"swift":      {name: "swift", lineComments: []string{"//"}, nestedComments: true, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"func": "function", "class": "class", "struct": "struct", "enum": "enum", "protocol": "interface", "extension": "impl", "actor": "class", "typealias": "type"}, notCalls: cControl},
	"php":        {name: "php", lineComments: []string{"//", "#"}, blockComments: [][2]string{{"/*", "*/"}}, identExtra: "$", decls: map[string]string{"function": "function", "class": "class", "interface": "interface", "trait": "interface", "enum": "enum"}, notCalls: cControl},
	"ruby":       {name: "ruby", family: indent, lineComments: []string{"#"}, identSuffix: "?!", decls: map[string]string{"def": "function", "class": "class", "module": "namespace"}, notCalls: cControl},
	"crystal":    {name: "crystal", family: indent, lineComments: []string{"#"}, identSuffix: "?!", decls: map[string]string{"def": "function", "class": "class", "module": "namespace", "struct": "struct", "macro": "macro", "lib": "namespace", "enum": "enum"}, notCalls: cControl},
	"dart":       {name: "dart", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"class": "class", "mixin": "class", "enum": "enum", "extension": "impl", "typedef": "type"}, cLike: true, notCalls: cControl},
	"scala":      {name: "scala", lineComments: []string{"//"}, nestedComments: true, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, lifetimes: true, decls: map[string]string{"def": "function", "class": "class", "object": "class", "trait": "interface", "enum": "enum", "type": "type"}, notCalls: cControl},
	"lua":        {name: "lua", family: indent, lineComments: []string{"--"}, blockComments: [][2]string{{"--[[", "]]"}}, longBrackets: true, decls: map[string]string{"function": "function"}, arrows: true, notCalls: cControl},
	"shell":      {name: "shell", lineComments: []string{"#"}, decls: map[string]string{"function": "function"}, notCalls: words("if then else elif fi for while until do done case esac in function select time")},
	"zig":        {name: "zig", lineComments: []string{"//"}, decls: map[string]string{"fn": "function", "test": "test"}, arrows: true, notCalls: cControl},
	"perl":       {name: "perl", lineComments: []string{"#"}, identExtra: "$@%", decls: map[string]string{"sub": "function", "package": "namespace"}, notCalls: cControl},
	"elixir":     {name: "elixir", family: indent, lineComments: []string{"#"}, tripleQuotes: true, identSuffix: "?!", decls: map[string]string{"def": "function", "defp": "function", "defmacro": "macro", "defmodule": "namespace", "defprotocol": "interface", "defimpl": "impl"}, notCalls: cControl},
	"julia":      {name: "julia", family: indent, lineComments: []string{"#"}, blockComments: [][2]string{{"#=", "=#"}}, tripleQuotes: true, identSuffix: "!", decls: map[string]string{"function": "function", "struct": "struct", "module": "namespace", "macro": "macro", "abstract": "type"}, notCalls: cControl},
	"r":          {name: "r", lineComments: []string{"#"}, arrows: true, notCalls: cControl},
	"groovy":     {name: "groovy", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, tripleQuotes: true, decls: map[string]string{"class": "class", "interface": "interface", "enum": "enum", "trait": "interface", "def": "function?"}, cLike: true, notCalls: cControl},
	"objc":       {name: "objc", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, decls: map[string]string{"interface": "class", "implementation": "class", "protocol": "interface", "struct": "struct", "enum": "enum"}, cLike: true, notCalls: cControl},
	"haskell": {
		name: "haskell", family: indent, lineComments: []string{"--"}, blockComments: [][2]string{{"{-", "-}"}}, nestedComments: true, identExtra: "'",
		decls: map[string]string{"data": "type", "newtype": "type", "type": "type", "class": "interface"}, declSkip: words("family instance"),
		clauses: true, juxtapose: true, notCalls: mlControl,
	},
	"elm": {
		name: "elm", family: indent, lineComments: []string{"--"}, blockComments: [][2]string{{"{-", "-}"}}, nestedComments: true,
		decls: map[string]string{"type": "type", "port": "function"}, declSkip: words("alias"), clauses: true, juxtapose: true, notCalls: mlControl,
	},
	"ocaml": {
		name: "ocaml", family: indent, blockComments: [][2]string{{"(*", "*)"}}, nestedComments: true, identExtra: "'",
		decls:    map[string]string{"let": "function", "type": "type", "module": "namespace", "class": "class", "exception": "type", "val": "function", "external": "function"},
		topLevel: words("let val external"), declSkip: words("rec nonrec mutable private virtual type"), juxtapose: true, notCalls: mlControl,
	},
	"fsharp": {
		name: "fsharp", family: indent, lineComments: []string{"//"}, blockComments: [][2]string{{"(*", "*)"}}, tripleQuotes: true, identExtra: "'",
		decls:    map[string]string{"let": "function", "type": "type", "module": "namespace", "namespace": "namespace", "member": "method", "exception": "type"},
		topLevel: words("let"), declSkip: words("rec inline private internal public mutable static abstract override default"), selfPrefix: true,
		juxtapose: true, notCalls: mlControl,
	},
	"clojure": {
		name: "clojure", family: braces, lineComments: []string{";"}, quoteIsPunct: true, identExtra: "-?!*+<>=/:.&%",
		decls: map[string]string{"defn": "function", "defn-": "function", "defmacro": "macro", "defprotocol": "interface", "defrecord": "struct", "deftype": "type", "defmulti": "function", "definterface": "interface"},
		lisp:  true, notCalls: lispControl,
	},
	"lisp": {
		name: "lisp", family: braces, lineComments: []string{";"}, blockComments: [][2]string{{"#|", "|#"}}, nestedComments: true, quoteIsPunct: true, identExtra: "-?!*+<>=/:.&%",
		decls: map[string]string{"defun": "function", "defmacro": "macro", "defclass": "class", "defstruct": "struct", "defgeneric": "function", "define": "function", "define-syntax": "macro", "define-record-type": "struct", "cl-defun": "function"},
		lisp:  true, notCalls: lispControl,
	},
	"erlang": {
		name: "erlang", family: indent, lineComments: []string{"%"}, clauses: true,
		decls: map[string]string{}, notCalls: words("if case receive of end when fun try catch after begin and or not andalso orelse div rem band bor bxor bnot bsl bsr xor"),
	},
	"gleam": {
		name: "gleam", lineComments: []string{"//"}, decls: map[string]string{"fn": "function", "type": "type", "const": "const"}, declSkip: words("pub opaque"), notCalls: cControl,
	},
	"nim": {
		name: "nim", family: indent, lineComments: []string{"#"}, blockComments: [][2]string{{"#[", "]#"}}, nestedComments: true, tripleQuotes: true,
		decls: map[string]string{"proc": "function", "func": "function", "method": "method", "iterator": "function", "template": "macro", "macro": "macro", "converter": "function"}, notCalls: cControl,
	},
	"vlang": {
		name: "vlang", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, backtickStrings: true,
		decls: map[string]string{"fn": "function", "struct": "struct", "interface": "interface", "enum": "enum", "type": "type", "union": "struct"}, declSkip: words("pub mut"), receivers: true, notCalls: cControl,
	},
	"solidity": {
		name: "solidity", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}},
		decls:    map[string]string{"contract": "class", "interface": "interface", "library": "class", "struct": "struct", "enum": "enum", "event": "type", "modifier": "function", "function": "function", "error": "type"},
		declSkip: words("abstract"), notCalls: merge(cControl, words("require revert emit assert keccak256 abi returns")),
	},
	"verilog": {
		name: "verilog", family: indent, lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}, endPrefix: true,
		decls:    map[string]string{"module": "module", "macromodule": "module", "interface": "interface", "package": "namespace", "class": "class", "function": "function", "task": "function", "program": "module"},
		declSkip: words("automatic static virtual"), notCalls: words("if for while case begin end assign always always_ff always_comb always_latch initial posedge negedge or and not repeat forever wait return input output inout wire reg logic integer parameter localparam genvar generate"),
	},
	"vhdl": {
		name: "vhdl", family: indent, lineComments: []string{"--"}, caseInsensitive: true, endPrefix: true, continues: words("begin is"),
		decls:    map[string]string{"entity": "module", "package": "namespace", "function": "function", "procedure": "function", "type": "type"},
		declSkip: words("body pure impure"), notCalls: words("if then else elsif for while case when loop process begin end return and or not xor nand nor others port map generic signal variable constant"),
	},
	"gdscript": {
		name: "gdscript", family: indent, lineComments: []string{"#"}, tripleQuotes: true,
		decls: map[string]string{"func": "function", "class": "class", "signal": "type", "enum": "enum", "class_name": "class"}, notCalls: merge(cControl, words("preload load print push_error assert")),
	},
	"powershell": {
		name: "powershell", lineComments: []string{"#"}, blockComments: [][2]string{{"<#", "#>"}}, caseInsensitive: true, identExtra: "-$:",
		decls: map[string]string{"function": "function", "filter": "function", "workflow": "function", "configuration": "function", "class": "class", "enum": "enum"}, commands: true,
		notCalls: words("if elseif else for foreach while do until switch try catch finally trap throw return break continue param begin process end in function filter class enum using exit"),
	},
	"vb": {
		name: "vb", family: indent, lineComments: []string{"'"}, caseInsensitive: true, endPrefix: true,
		decls:    map[string]string{"sub": "function", "function": "function", "class": "class", "module": "namespace", "structure": "struct", "interface": "interface", "enum": "enum", "namespace": "namespace", "property": "method"},
		declSkip: words("readonly writeonly default"), notCalls: words("if then else elseif for each next while wend do loop until select case with end sub function return exit call dim set let new not and or andalso orelse is isnot mod typeof ctype directcast trycast gettype"),
	},
	"pascal": {
		name: "pascal", family: indent, lineComments: []string{"//"}, blockComments: [][2]string{{"(*", "*)"}, {"{", "}"}}, caseInsensitive: true, endPrefix: true,
		continues: words("var const label begin type"),
		decls:     map[string]string{"procedure": "function", "function": "function", "constructor": "function", "destructor": "function", "program": "namespace", "unit": "namespace"},
		declSkip:  words("class"), notCalls: words("if then else for to downto do while repeat until case of begin end with try except finally raise inherited exit not and or div mod in is as"),
	},
	"fortran": {
		name: "fortran", family: indent, lineComments: []string{"!"}, caseInsensitive: true, endPrefix: true, continues: words("contains"),
		decls:    map[string]string{"subroutine": "function", "function": "function", "module": "namespace", "program": "namespace", "submodule": "namespace", "interface": "interface", "type": "type"},
		declSkip: words("recursive pure elemental impure procedure"), notCalls: words("if then else elseif do while select case where forall call allocate deallocate write read print open close format return stop exit cycle size len trim allocated present associated real int dble abs sqrt exp log sin cos tan max min mod sum result intent dimension kind"),
	},
	"sql":       {name: "sql", lineComments: []string{"--"}, blockComments: [][2]string{{"/*", "*/"}}, backtickStrings: true, caseInsensitive: true},
	"graphql":   {name: "graphql", lineComments: []string{"#"}, tripleQuotes: true},
	"protobuf":  {name: "protobuf", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}},
	"thrift":    {name: "thrift", lineComments: []string{"//", "#"}, blockComments: [][2]string{{"/*", "*/"}}},
	"prisma":    {name: "prisma", lineComments: []string{"//"}},
	"terraform": {name: "terraform", lineComments: []string{"#", "//"}, blockComments: [][2]string{{"/*", "*/"}}, identExtra: "-"},
	"godot":     {name: "godot", lineComments: []string{";"}},
	"css":       {name: "css", blockComments: [][2]string{{"/*", "*/"}}},
	"scss":      {name: "scss", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}},
	"less":      {name: "less", lineComments: []string{"//"}, blockComments: [][2]string{{"/*", "*/"}}},
	"html":      {name: "html"},
	"template":  {name: "template"},
	"markdown":  {name: "markdown"},
	"rst":       {name: "rst"},
	"asciidoc":  {name: "asciidoc"},
	"notebook":  {name: "notebook"},
	"npm":       {name: "npm"},
	"make":      {name: "make"},
	"just":      {name: "just"},
	"taskfile":  {name: "taskfile"},
	"docker":    {name: "docker"},
	"compose":   {name: "compose"},
	"workflow":  {name: "workflow"},
	"gitlab-ci": {name: "gitlab-ci"},
	"pyproject": {name: "pyproject"},
	"cargo":     {name: "cargo"},
	"openapi":   {name: "openapi"},
}

// extensions maps a lowercase file extension to its language.
var extensions = map[string]string{
	".ts": "typescript", ".tsx": "typescript", ".mts": "typescript", ".cts": "typescript",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".vue": "typescript", ".svelte": "typescript", ".astro": "typescript",
	".py": "python", ".pyi": "python", ".mojo": "mojo",
	".java": "java", ".kt": "kotlin", ".kts": "kotlin",
	".cs": "csharp",
	".c":  "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp", ".hxx": "cpp", ".cu": "cpp", ".cuh": "cpp",
	".hlsl": "cpp", ".hlsli": "cpp", ".fx": "cpp", ".metal": "cpp",
	".glsl": "glsl", ".vert": "glsl", ".frag": "glsl", ".geom": "glsl", ".comp": "glsl", ".tesc": "glsl", ".tese": "glsl",
	".wgsl": "wgsl",
	".rs":   "rust", ".swift": "swift", ".php": "php", ".rb": "ruby", ".cr": "crystal", ".dart": "dart",
	".scala": "scala", ".sc": "scala", ".lua": "lua",
	".sh": "shell", ".bash": "shell", ".zsh": "shell",
	".zig": "zig", ".pl": "perl", ".pm": "perl", ".ex": "elixir", ".exs": "elixir",
	".jl": "julia", ".r": "r", ".groovy": "groovy", ".gradle": "groovy", ".m": "objc", ".mm": "objc",
	".go": "go",
	".hs": "haskell", ".lhs": "haskell", ".elm": "elm", ".ml": "ocaml", ".mli": "ocaml",
	".fs": "fsharp", ".fsi": "fsharp", ".fsx": "fsharp",
	".clj": "clojure", ".cljs": "clojure", ".cljc": "clojure",
	".lisp": "lisp", ".lsp": "lisp", ".el": "lisp", ".scm": "lisp", ".ss": "lisp", ".rkt": "lisp",
	".erl": "erlang", ".hrl": "erlang", ".gleam": "gleam", ".nim": "nim",
	".sol": "solidity", ".v": "verilog", ".sv": "verilog", ".svh": "verilog", ".vh": "verilog",
	".vhd": "vhdl", ".vhdl": "vhdl", ".gd": "gdscript", ".tscn": "godot", ".tres": "godot",
	".ps1": "powershell", ".psm1": "powershell", ".vb": "vb", ".bas": "vb", ".vbs": "vb",
	".pas": "pascal", ".pp": "pascal", ".dpr": "pascal", ".lpr": "pascal",
	".f90": "fortran", ".f95": "fortran", ".f03": "fortran", ".f08": "fortran", ".f": "fortran", ".for": "fortran", ".f77": "fortran",
	".sql": "sql", ".graphql": "graphql", ".gql": "graphql", ".proto": "protobuf", ".thrift": "thrift", ".prisma": "prisma",
	".tf":  "terraform",
	".css": "css", ".scss": "scss", ".sass": "scss", ".less": "less",
	".html": "html", ".htm": "html", ".xhtml": "html",
	".hbs": "template", ".handlebars": "template", ".ejs": "template", ".jinja": "template", ".jinja2": "template", ".j2": "template",
	".njk": "template", ".twig": "template", ".erb": "template", ".liquid": "template", ".mustache": "template",
	".md": "markdown", ".markdown": "markdown", ".mdx": "markdown", ".mdown": "markdown", ".rst": "rst", ".adoc": "asciidoc", ".asciidoc": "asciidoc",
	".ipynb": "notebook", ".dockerfile": "docker", ".mk": "make",
}

