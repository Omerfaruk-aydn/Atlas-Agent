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

