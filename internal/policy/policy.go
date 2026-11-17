package policy

import (
	"path"
	"strings"
	"sync"
)

// Action is what the policy wants done with a command.
type Action int

const (
	// Allow lets the command run under the normal permission flow.
	Allow Action = iota
	// Ask forces a permission prompt even where a shortcut would skip it.
	Ask
	// Deny refuses the command.
	Deny
)

func (a Action) String() string {
	switch a {
	case Ask:
		return "ask"
	case Deny:
		return "deny"
	}
	return "allow"
}

// Finding is one rule that matched.
type Finding struct {
	RuleID string
	Action Action
	Reason string
	Remedy string
	// Enforced is false for a rule still in log-only rollout: it is
	// recorded but does not change the verdict.
	Enforced bool
	// Grant is the exact grant string that would allow an Ask finding.
	Grant string
	// Command is the argv of the call that matched, for the log.
	Command string
}

// Verdict is the combined outcome for a command.
type Verdict struct {
	Action   Action
	Findings []Finding
}

// Blocking returns the enforced finding that decided the verdict.
func (v Verdict) Blocking() *Finding {
	var best *Finding
	for i := range v.Findings {
		f := &v.Findings[i]
		if !f.Enforced || f.Action == Allow {
			continue
		}
		if best == nil || f.Action > best.Action {
			best = f
		}
	}
	return best
}

// Message explains a refusal or prompt, with the remedy and, for asks, the
// exact grant that would permit the command.
func (v Verdict) Message() string {
	f := v.Blocking()
	if f == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(f.Reason)
	b.WriteString(" [rule ")
	b.WriteString(f.RuleID)
	b.WriteString("]")
	if f.Remedy != "" {
		b.WriteString(" ")
		b.WriteString(f.Remedy)
	}
	if f.Grant != "" {
		b.WriteString(" To allow exactly this, grant: ")
		b.WriteString(f.Grant)
	}
	return b.String()
}

// Context is what the rules may know about the session.
type Context struct {
	// Dir is the working directory, used to resolve relative paths.
	Dir string
	// Branch is the current git branch, if known.
	Branch string
	// ProtectedBranches overrides the default protected list.
	ProtectedBranches []string
}

// Rule is one named check. Match is given each call in order.
type Rule struct {
	ID     string
	Action Action
	Reason string
	Remedy string
	// Enforce is false while a rule is being rolled out: it logs only.
	Enforce bool
	Match   func(c Call, x *Context) bool
}

// Engine evaluates commands. It is safe for concurrent use.
type Engine struct {
	mu          sync.Mutex
	rules       []Rule
	constraints []Constraint
	grants      []Grant
	taint       *Taint
	audit       *Audit
}

// New returns an engine with the built-in rules, a taint tracker and an
// audit log (file may be empty for in-memory only).
func New(auditFile string) *Engine {
	return &Engine{rules: builtinRules(), taint: NewTaint(), audit: NewAudit(auditFile)}
}

// AddConstraints installs user constraints such as "do not push".
func (e *Engine) AddConstraints(cs ...Constraint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.constraints = append(e.constraints, cs...)
}

// Grant installs a scoped grant that turns matching Ask findings into Allow.
func (e *Engine) Grant(g Grant) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.grants = append(e.grants, g)
}

// Taint exposes the download tracker, for persistence across compaction.
func (e *Engine) Taint() *Taint { return e.taint }

// Audit exposes the denial log.
func (e *Engine) Audit() *Audit { return e.audit }

// Rules lists the rule IDs, for documentation and tests.
func (e *Engine) Rules() []Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Rule(nil), e.rules...)
}

// Check evaluates a shell command. A command that does not parse is asked
// about rather than allowed, since the policy cannot see inside it.
func (e *Engine) Check(command string, x Context) Verdict {
	e.mu.Lock()
	defer e.mu.Unlock()
	calls, ok := Parse(command)
	if !ok {
		v := Verdict{Action: Ask, Findings: []Finding{{
			RuleID: "unparseable-command", Action: Ask, Enforced: true,
			Reason: "The command is not valid shell, so its effect cannot be checked.",
			Remedy: "Rewrite it as a plain command.",
		}}}
		e.audit.record(command, v)
		return v
	}
	return e.checkCalls(command, calls, x)
}

// CheckArgv evaluates a program run with an explicit argument vector. No
// shell is involved, so the arguments are never re-parsed as shell text:
// a value such as "; rm -rf /" is just a string. Wrappers like sh -c are
// still looked into, because they do start a shell.
func (e *Engine) CheckArgv(argv []string, x Context) Verdict {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.checkCalls(strings.Join(argv, " "), unwrap(Call{Argv: argv}, 0), x)
}

func (e *Engine) checkCalls(command string, calls []Call, x Context) Verdict {
	var findings []Finding
	for _, c := range calls {
		if len(c.Argv) == 0 && len(c.Redirects) == 0 {
			continue
		}
		for _, r := range e.rules {
			if r.Match(c, &x) {
				findings = append(findings, Finding{RuleID: r.ID, Action: r.Action, Reason: r.Reason, Remedy: r.Remedy, Enforced: r.Enforce, Command: strings.Join(c.Argv, " ")})
			}
		}
		findings = append(findings, e.taint.check(c, x.Dir)...)
		for _, k := range e.constraints {
			if k.Matches(c) {
				findings = append(findings, Finding{RuleID: "user-constraint", Action: Deny, Enforced: true, Reason: "You told me not to: " + k.Text + ".", Remedy: "Ask the user to lift the constraint first.", Command: strings.Join(c.Argv, " ")})
			}
		}
		// The matrix for pushes depends on the session, so it is not a Rule.
		if f := pushMatrix(c, &x); f != nil {
			findings = append(findings, *f)
		}
		// A later call in the same command sees files this one fetched.
		e.taint.observe(c, x.Dir)
	}
	for i := range findings {
		f := &findings[i]
		if f.Action != Ask {
			continue
		}
		grant := grantString(f.Command)
		f.Grant = grant
		for _, g := range e.grants {
			if g.Allows(f.Command) {
				f.Action = Allow
				break
			}
		}
	}
	v := Verdict{Findings: findings}
	for _, f := range findings {
		if f.Enforced && f.Action > v.Action {
			v.Action = f.Action
		}
	}
	if len(findings) > 0 {
		e.audit.record(command, v)
	}
	return v
}

// protected is the default list of branches a push should not touch
// unasked.
var defaultProtected = []string{"main", "master", "trunk", "develop", "production", "prod", "release"}

func (x *Context) isProtected(branch string) bool {
	list := x.ProtectedBranches
	if len(list) == 0 {
		list = defaultProtected
	}
	branch = strings.TrimPrefix(branch, "refs/heads/")
	for _, p := range list {
		if branch == p || strings.HasPrefix(branch, p+"/") && p == "release" {
			return true
		}
		if ok, _ := path.Match(p, branch); ok {
			return true
		}
	}
	return false
}
