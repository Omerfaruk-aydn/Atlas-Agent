// Package policy decides whether a shell command may run, from the parsed
// command rather than its text. It unwraps sh -c, env, sudo and similar
// wrappers, evaluates named rules (each with a reason and a remedy), applies
// user constraints such as "do not push", tracks files that came from the
// network so they cannot be executed unreviewed, and keeps an audit log.
package policy

import (
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Call is one simple command: the program and its arguments, with the
// wrappers around it removed.
type Call struct {
	Argv []string
	// Dynamic is true when some word is built from an expansion the parser
	// cannot resolve, such as $VAR or $(...). Rules treat such words
	// conservatively.
	Dynamic bool
	// Redirects holds the targets of output redirections (> and >>).
	Redirects []string
	// PipedFrom is the program of the command feeding this one through a
	// pipe, if any.
	PipedFrom string
	// Wrapper names the wrappers that were removed, outermost first.
	Wrapper []string
}

// Name is the program's base name without a Windows extension.
func (c Call) Name() string {
	if len(c.Argv) == 0 {
		return ""
	}
	n := strings.ToLower(path.Base(strings.ReplaceAll(c.Argv[0], `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		n = strings.TrimSuffix(n, ext)
	}
	return n
}

// Args are the arguments after the program.
func (c Call) Args() []string {
	if len(c.Argv) < 2 {
		return nil
	}
	return c.Argv[1:]
}

const maxUnwrapDepth = 4

// Parse returns every simple command in src, including those inside
// command substitutions, subshells and wrapper strings. It returns ok=false
// when src is not valid shell; callers should then treat the command as
// unknown rather than safe.
func Parse(src string) (calls []Call, ok bool) {
	return parseDepth(src, 0)
}

func parseDepth(src string, depth int) ([]Call, bool) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return nil, false
	}
	var calls []Call
	pipedFrom := map[*syntax.CallExpr]string{}
	syntax.Walk(file, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.BinaryCmd:
			if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
				if from := edgeCall(x.X, true); from != nil {
					if to := edgeCall(x.Y, false); to != nil {
						pipedFrom[to] = programOf(from)
					}
				}
			}
		case *syntax.Stmt:
			ce, ok := x.Cmd.(*syntax.CallExpr)
			if !ok {
				return true
			}
			var targets []string
			for _, r := range x.Redirs {
				switch r.Op {
				case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll:
					if s, _ := wordString(r.Word); s != "" {
						targets = append(targets, s)
					}
				}
			}
			call := buildCall(ce)
			call.PipedFrom = pipedFrom[ce]
			expanded := unwrap(call, depth)
			for i := range expanded {
				expanded[i].Redirects = append(expanded[i].Redirects, targets...)
			}
			calls = append(calls, expanded...)
		}
		return true
	})
	return calls, true
}

// edgeCall finds the simple command at one end of a pipeline: the last
// stage when last is true, the first stage otherwise.
func edgeCall(s *syntax.Stmt, last bool) *syntax.CallExpr {
	if s == nil {
		return nil
	}
	switch x := s.Cmd.(type) {
	case *syntax.CallExpr:
		return x
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			if last {
				return edgeCall(x.Y, last)
			}
			return edgeCall(x.X, last)
		}
	}
	return nil
}

func programOf(c *syntax.CallExpr) string {
	call := buildCall(c)
	return call.Name()
}

func buildCall(ce *syntax.CallExpr) Call {
	var call Call
	for _, w := range ce.Args {
		s, dynamic := wordString(w)
		call.Dynamic = call.Dynamic || dynamic
		call.Argv = append(call.Argv, s)
	}
	return call
}

// wordString flattens a word to text. Expansions become "$?" markers and set
// the dynamic flag.
func wordString(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var b strings.Builder
	dynamic := false
	var walk func(parts []syntax.WordPart)
	walk = func(parts []syntax.WordPart) {
		for _, p := range parts {
			switch x := p.(type) {
			case *syntax.Lit:
				b.WriteString(x.Value)
			case *syntax.SglQuoted:
				b.WriteString(x.Value)
			case *syntax.DblQuoted:
				walk(x.Parts)
			default:
				dynamic = true
				b.WriteString("$?")
			}
		}
	}
	walk(w.Parts)
	return b.String(), dynamic
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

var shellNames = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

var powershellNames = map[string]bool{"powershell": true, "pwsh": true}

// unwrap strips env/sudo/... wrappers and expands sh -c and PowerShell
// -Command strings into their own calls. The wrapped call keeps its pipe
// source and records which wrappers were removed.
func unwrap(c Call, depth int) []Call {
	out := []Call{}
	cur := c
	for len(cur.Argv) > 0 && depth <= maxUnwrapDepth {
		name := cur.Name()
		rest := cur.Args()
		switch name {
		case "env":
			i := 0
			for i < len(rest) && (assignRe.MatchString(rest[i]) || strings.HasPrefix(rest[i], "-")) {
				i++
			}
			cur = rewrap(cur, name, rest[i:])
			continue
		case "sudo", "doas":
			i := 0
			for i < len(rest) && strings.HasPrefix(rest[i], "-") {
				i++
				if i < len(rest) && (rest[i-1] == "-u" || rest[i-1] == "-g") {
					i++
				}
			}
			cur = rewrap(cur, name, rest[i:])
			continue
		case "nohup", "command", "exec", "time", "builtin", "stdbuf", "nice", "ionice":
			i := 0
			for i < len(rest) && strings.HasPrefix(rest[i], "-") {
				i++
			}
			cur = rewrap(cur, name, rest[i:])
			continue
		case "timeout":
			i := 0
			for i < len(rest) && strings.HasPrefix(rest[i], "-") {
				i++
			}
			if i < len(rest) {
				i++ // The duration.
			}
			cur = rewrap(cur, name, rest[min(i, len(rest)):])
			continue
		}
		break
	}
	if len(cur.Argv) == 0 {
		return append(out, cur)
	}
	name := cur.Name()
	if shellNames[name] {
		if script := dashCArg(cur.Args()); script != "" && depth < maxUnwrapDepth {
			if inner, ok := parseDepth(script, depth+1); ok {
				for i := range inner {
					if inner[i].PipedFrom == "" {
						inner[i].PipedFrom = cur.PipedFrom
					}
					inner[i].Wrapper = append(append([]string{}, cur.Wrapper...), append([]string{name}, inner[i].Wrapper...)...)
				}
				return append(out, inner...)
			}
		}
	}
	if powershellNames[name] {
		if script := powershellCommand(cur.Args()); script != "" && depth < maxUnwrapDepth {
			for _, part := range splitPowerShell(script) {
				inner := Call{Argv: part.argv, PipedFrom: part.pipedFrom, Wrapper: append(append([]string{}, cur.Wrapper...), name)}
				out = append(out, inner)
			}
			return out
		}
	}
	return append(out, cur)
}

func rewrap(c Call, wrapper string, rest []string) Call {
	c.Wrapper = append(c.Wrapper, wrapper)
	c.Argv = rest
	return c
}

func dashCArg(args []string) string {
	for i, a := range args {
		if a == "-c" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c")) {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

func powershellCommand(args []string) string {
	for i, a := range args {
		switch strings.ToLower(a) {
		case "-c", "-command":
			return strings.Join(args[i+1:], " ")
		}
	}
	return ""
}

// psStage is one PowerShell command and the program that fed it.
type psStage struct {
	argv      []string
	pipedFrom string
}

// splitPowerShell is a deliberately small tokenizer: it separates a script
// into pipeline stages and statements and splits words on spaces, honoring
// simple quotes. Rules only look at the program name and flags.
func splitPowerShell(script string) []psStage {
	var stages []psStage
	var cur []string
	var word strings.Builder
	var quote rune
	pipeNext := false
	prev := ""
	flushWord := func() {
		if word.Len() > 0 {
			cur = append(cur, word.String())
			word.Reset()
		}
	}
	flushCall := func(pipe bool) {
		flushWord()
		if len(cur) > 0 {
			st := psStage{argv: cur}
			if pipeNext {
				st.pipedFrom = prev
			}
			stages = append(stages, st)
			prev = Call{Argv: cur}.Name()
		}
		pipeNext = pipe && len(cur) > 0
		cur = nil
	}
	for _, r := range script {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flushWord()
		case r == ';' || r == '|':
			flushCall(r == '|')
		default:
			word.WriteRune(r)
		}
	}
	flushCall(false)
	return stages
}
