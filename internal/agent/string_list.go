package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// stringList is a list of strings that also accepts the shapes models
// actually send when a tool asks for one.
//
// The schema says "array of string" and plenty of models send exactly
// that. Others send the array JSON-encoded inside a string --
// `"[\"review\",\"security\"]"` -- and others again send one
// comma-separated string. Refusing those is technically correct and
// practically useless: the caller had the right idea, the tool call
// fails on a quoting detail, and the user sees an unmarshalling error
// about a struct field they have never heard of.
//
// So accept all three. The cost is a few lines here; the alternative is
// a tool that works on some models and not others for no reason anyone
// can act on.
type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	// The ordinary case, and the only one the schema asks for.
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*s = list
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("expected a list of strings, got %s", string(data))
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		*s = nil
		return nil
	}

	// An array that arrived quoted.
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &list); err == nil {
			*s = list
			return nil
		}
	}

	// One name, or several separated by commas. Splitting an ordinary
	// single value on commas is harmless: a subagent name has no comma
	// in it, so a string without one comes back as itself.
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	*s = list
	return nil
}

// taskList is delegate's list of subtasks, tolerant of the same
// stringified-array shape stringList handles. Only the outer quoting is
// forgiven: a task whose own fields are wrong still fails, and should.
type taskList []DelegateTask

func (t *taskList) UnmarshalJSON(data []byte) error {
	var list []DelegateTask
	if err := json.Unmarshal(data, &list); err == nil {
		*t = list
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("expected a list of tasks, got %s", string(data))
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &list); err != nil {
		return fmt.Errorf("expected a list of tasks, got the string %q", raw)
	}
	*t = list
	return nil
}
