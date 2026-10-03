package session

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var taskIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ValidateTaskGraph validates identities, dependencies and executable ownership.
func ValidateTaskGraph(todos []Todo) error {
	if len(todos) > 256 {
		return fmt.Errorf("at most 256 tasks are supported")
	}
	byID := map[string]Todo{}
	for _, t := range todos {
		if len(t.Agent) > 64 {
			return fmt.Errorf("agent name cannot exceed 64 bytes")
		}
		if t.ID == "" {
			if len(t.DependsOn) > 0 {
				return fmt.Errorf("dependencies require a task ID")
			}
			continue
		}
		if !taskIDPattern.MatchString(t.ID) {
			return fmt.Errorf("invalid task ID %q", t.ID)
		}
		if _, ok := byID[t.ID]; ok {
			return fmt.Errorf("duplicate task ID %q", t.ID)
		}
		if len(t.DependsOn) > 32 || len(t.OwnedPaths) > 32 {
			return fmt.Errorf("task %s has too many dependencies or paths", t.ID)
		}
		for _, p := range t.OwnedPaths {
			p = strings.ReplaceAll(p, `\`, "/")
			if len(p) > 512 || strings.ContainsAny(p, "*?:") || strings.HasPrefix(p, "/") || path.Clean(p) == ".." || strings.HasPrefix(path.Clean(p), "../") || p == "" {
				return fmt.Errorf("ownership paths must be literal relative paths within the project")
			}
		}
		byID[t.ID] = t
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("dependency cycle at %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range byID[id].DependsOn {
			d, ok := byID[dep]
			if !ok {
				return fmt.Errorf("task %s depends on unknown task %s", id, dep)
			}
			if byID[id].Status != TodoStatusPending && d.Status != TodoStatusCompleted {
				return fmt.Errorf("task %s cannot start or complete before dependency %s", id, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func OwnershipOverlaps(a, b []string) bool {
	// Empty ownership is conservatively treated as the whole repository.
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	for _, x := range a {
		for _, y := range b {
			x = strings.ToLower(path.Clean(strings.ReplaceAll(x, `\`, "/")))
			y = strings.ToLower(path.Clean(strings.ReplaceAll(y, `\`, "/")))
			if x == "." || y == "." || x == y || strings.HasPrefix(x, y+"/") || strings.HasPrefix(y, x+"/") {
				return true
			}
		}
	}
	return false
}

// ReadyTaskWave selects deterministic ready tasks with disjoint write ownership.
func ReadyTaskWave(todos []Todo, limit int) ([]Todo, error) {
	return ReadyTaskWaveExcluding(todos, limit, nil)
}

// ReadyTaskWaveExcluding retains held dependency records without dispatching
// them or treating an idle held task as a writer.
func ReadyTaskWaveExcluding(todos []Todo, limit int, held map[string]bool) ([]Todo, error) {
	if err := ValidateTaskGraph(todos); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 16 {
		limit = 4
	}
	completed := map[string]bool{}
	for _, t := range todos {
		completed[t.ID] = t.Status == TodoStatusCompleted
	}
	var wave []Todo
	for _, t := range todos {
		if t.ID == "" || t.Status != TodoStatusPending || held[t.ID] {
			continue
		}
		ready := true
		for _, dep := range t.DependsOn {
			if !completed[dep] {
				ready = false
			}
		}
		for _, other := range todos {
			if other.Status == TodoStatusInProgress && OwnershipOverlaps(t.OwnedPaths, other.OwnedPaths) {
				ready = false
			}
		}
		for _, other := range wave {
			if OwnershipOverlaps(t.OwnedPaths, other.OwnedPaths) {
				ready = false
			}
		}
		if ready {
			wave = append(wave, t)
		}
		if len(wave) >= limit {
			break
		}
	}
	return wave, nil
}
