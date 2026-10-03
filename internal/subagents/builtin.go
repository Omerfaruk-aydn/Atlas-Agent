package subagents

import (
	"embed"
	"path"
	"slices"
	"strings"
	"sync"
)

// builtinFS holds the subagent definitions that ship inside the binary:
// the "modes" a user can put to work without authoring anything. Each is
// a specialist system prompt plus a "model" field naming the model role
// it runs on, so assigning a model to that role (see internal/config's
// ModelRoles) is the only setup a mode needs.
//
// They are also the catalog the session-mode switch draws from -- the
// same prompt either runs as a delegated subagent or is folded into the
// main session's own system prompt, so the two can never drift apart.
//
//go:embed builtin/*.md
var builtinFS embed.FS

// builtinOnce parses the embedded definitions the first time they are
// asked for. A file that fails to parse or validate is skipped rather
// than panicking: these are compiled in and covered by tests, so a bad
// one is a build-time bug and not worth crashing a user's session over.
var builtinOnce = sync.OnceValue(func() []Subagent {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil
	}

	var all []Subagent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), FileExt) {
			continue
		}
		content, err := builtinFS.ReadFile(path.Join("builtin", entry.Name()))
		if err != nil {
			continue
		}
		s, err := ParseContent(content)
		if err != nil {
			continue
		}
		if err := s.Validate(); err != nil {
			continue
		}
		s.Builtin = true
		all = append(all, *s)
	}

	slices.SortStableFunc(all, func(a, b Subagent) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return all
})

// Builtin returns the subagent definitions shipped with the binary,
// sorted by name. Each call returns fresh copies so a caller that
// mutates one (the Subagents dialog editing a mode into a real file,
// say) cannot corrupt the parsed-once catalog everyone else reads.
func Builtin() []*Subagent {
	src := builtinOnce()
	out := make([]*Subagent, len(src))
	for i := range src {
		s := src[i]
		out[i] = &s
		out[i].Tools = slices.Clone(s.Tools)
		if s.Contract != nil {
			contract := *s.Contract
			contract.TaskTypes = slices.Clone(contract.TaskTypes)
			contract.Responsibilities = slices.Clone(contract.Responsibilities)
			contract.Inputs = slices.Clone(contract.Inputs)
			contract.Outputs = slices.Clone(contract.Outputs)
			contract.Completion = slices.Clone(contract.Completion)
			contract.RequiredTools = slices.Clone(contract.RequiredTools)
			contract.DecisionRights = slices.Clone(contract.DecisionRights)
			contract.OutOfScope = slices.Clone(contract.OutOfScope)
			contract.StopConditions = slices.Clone(contract.StopConditions)
			contract.EvidenceRequired = slices.Clone(contract.EvidenceRequired)
			out[i].Contract = &contract
		}
	}
	return out
}
