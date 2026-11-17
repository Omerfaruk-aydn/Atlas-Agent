package routine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// AutomationPrefix marks routines that come from files. Sync owns routines
// with this prefix: it creates, updates and removes them to match the files.
const AutomationPrefix = "auto-"

// ParseAutomation reads one automation file:
//
//	---
//	schedule: CRON_TZ=Europe/Istanbul 0 9 * * 1-5
//	permissions: readonly        (readonly | edits | configured)
//	model: provider/model        (optional)
//	goal: true                   (optional)
//	timeout: 600                 (optional, seconds)
//	max_late: 3600               (optional, seconds)
//	---
//	The prompt to run.
//
// The file's base name becomes the routine name. A file cannot request the
// "auto" permission policy, which approves every tool call: that must be
// chosen by the user on the command line, never by a file in a repository.
func ParseAutomation(name, text, projectDir string) (Routine, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return Routine{}, fmt.Errorf("%s: missing front matter with a schedule", name)
	}
	head, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return Routine{}, fmt.Errorf("%s: front matter is not closed with ---", name)
	}
	r := Routine{Name: AutomationPrefix + name, Prompt: strings.TrimSpace(strings.TrimPrefix(body, "\n")), Dir: projectDir, Permissions: PermissionsReadOnly}
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "schedule":
			r.Schedule = value
		case "permissions":
			r.Permissions = value
		case "model":
			r.Model = value
		case "goal":
			r.Goal = value == "true"
		case "timeout":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Routine{}, fmt.Errorf("%s: timeout must be a number of seconds", name)
			}
			r.TimeoutSeconds = n
		case "max_late":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Routine{}, fmt.Errorf("%s: max_late must be a number of seconds", name)
			}
			r.MaxLateSeconds = n
		}
	}
	if r.Permissions == PermissionsAuto {
		return Routine{}, fmt.Errorf("%s: a file cannot request permissions: auto; create the routine with the routine command instead", name)
	}
	if err := r.Validate(); err != nil {
		return Routine{}, fmt.Errorf("%s: %w", name, err)
	}
	return r, nil
}

// SyncReport says what Sync did.
type SyncReport struct {
	Added     []string
	Updated   []string
	Removed   []string
	Unchanged []string
	Errors    []string
}

func sameDefinition(a, b Routine) bool {
	return a.Schedule == b.Schedule && a.Prompt == b.Prompt && a.Dir == b.Dir && a.Model == b.Model &&
		a.Goal == b.Goal && a.Permissions == b.Permissions && a.TimeoutSeconds == b.TimeoutSeconds && a.MaxLateSeconds == b.MaxLateSeconds
}

// Sync makes the store's file-defined routines match the *.md files in
// automationsDir. New routines start disabled so that a repository cannot
// schedule work on its own; enable one with the routine command after
// reading it. Updating a routine keeps its enabled state.
func Sync(ctx context.Context, store *Store, automationsDir, projectDir string) (SyncReport, error) {
	var report SyncReport
	desired := map[string]Routine{}
	entries, err := os.ReadDir(automationsDir)
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		data, err := os.ReadFile(filepath.Join(automationsDir, e.Name()))
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		r, err := ParseAutomation(base, string(data), projectDir)
		if err != nil {
			report.Errors = append(report.Errors, err.Error())
			continue
		}
		desired[r.Name] = r
	}
	existing, err := store.List(ctx)
	if err != nil {
		return report, err
	}
	have := map[string]Routine{}
	for _, r := range existing {
		if strings.HasPrefix(r.Name, AutomationPrefix) && r.Dir == projectDir {
			have[r.Name] = r
		}
	}
	names := make([]string, 0, len(desired))
	for n := range desired {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		want := desired[name]
		cur, found := have[name]
		switch {
		case !found:
			want.Enabled = false
			if _, err := store.Add(ctx, want); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			report.Added = append(report.Added, name)
		case sameDefinition(cur, want):
			report.Unchanged = append(report.Unchanged, name)
		default:
			if _, err := store.Remove(ctx, cur.ID); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			want.Enabled = cur.Enabled
			if _, err := store.Add(ctx, want); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", name, err))
				continue
			}
			report.Updated = append(report.Updated, name)
		}
	}
	var gone []string
	for name := range have {
		if _, ok := desired[name]; !ok {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		if _, err := store.Remove(ctx, have[name].ID); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		report.Removed = append(report.Removed, name)
	}
	return report, nil
}
