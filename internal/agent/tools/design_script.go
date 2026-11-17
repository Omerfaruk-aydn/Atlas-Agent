package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

// designScriptRetries is how often a script runs again after losing a save
// race to the person or another agent.
const designScriptRetries = 3

func designScriptName(p DesignCanvasParams) (string, error) {
	name := strings.TrimSpace(p.Script)
	if name == "" {
		return p.File, nil
	}
	if !design.ValidName(name) {
		return "", fmt.Errorf("invalid script name %q: use lower case letters, digits, '-', '_' and '.'", name)
	}
	return name, nil
}

// designScriptPaths are where a script and the system it declared live.
func designScriptPaths(workingDir, file, script string) (src, sys string) {
	base := file
	if script != file {
		base = file + "." + script
	}
	dir := filepath.Join(workingDir, ".atlas", "design")
	return filepath.Join(dir, "scripts", base+".js"), filepath.Join(dir, "systems", base+".json")
}

// loadDesignSystems reads every system the file's scripts declared.
func loadDesignSystems(workingDir, file string) []*design.System {
	dir := filepath.Join(workingDir, ".atlas", "design", "systems")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*design.System
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		if stem != file && !strings.HasPrefix(stem, file+".") {
			continue
		}
		var sys design.System
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || json.Unmarshal(data, &sys) != nil {
			continue
		}
		out = append(out, &sys)
	}
	return out
}

// mergedSystem is one view of all the file's systems, for lint.
func mergedSystem(list []*design.System) *design.System {
	if len(list) == 0 {
		return nil
	}
	out := &design.System{Radius: map[string]json.RawMessage{}}
	for _, s := range list {
		if len(s.Space) > len(out.Space) {
			out.Space = s.Space
		}
		for k, v := range s.Radius {
			out.Radius[k] = v
		}
		out.Components = append(out.Components, s.Components...)
		out.Screens = append(out.Screens, s.Screens...)
		if len(s.Colors) > len(out.Colors) {
			out.Colors = s.Colors
		}
		if len(s.Type) > len(out.Type) {
			out.Type = s.Type
		}
	}
	return out
}

func designScript(ctx context.Context, workingDir string, store *design.Store, p DesignCanvasParams) (string, error) {
	if !design.ValidName(p.File) {
		return "", fmt.Errorf("file is required: the design the script builds into (it is created when it does not exist)")
	}
	script, err := designScriptName(p)
	if err != nil {
		return "", err
	}
	srcPath, sysPath := designScriptPaths(workingDir, p.File, script)
	src := p.Code
	if strings.TrimSpace(src) == "" {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return "", fmt.Errorf("script needs code (the JavaScript), or an earlier script saved at %s to run again", relTo(workingDir, srcPath))
		}
		src = string(data)
	}
	var prev *design.System
	if data, err := os.ReadFile(sysPath); err == nil {
		var s design.System
		if json.Unmarshal(data, &s) == nil {
			prev = &s
		}
	}
	for attempt := 0; ; attempt++ {
		doc, rev, err := store.Load(p.File)
		create := errors.Is(err, os.ErrNotExist)
		if create {
			title := p.Title
			if title == "" {
				title = p.File
			}
			doc, rev, err = design.New(title), "", nil
		}
		if err != nil {
			return "", err
		}
		res, err := design.RunScript(ctx, doc, prev, src, design.ScriptOptions{Name: script, Force: p.Force})
		if err != nil {
			var se *design.ScriptError
			if errors.As(err, &se) {
				return "", fmt.Errorf("the script failed, nothing changed: %s", se.Msg)
			}
			return "", err
		}
		blocking, warnings := designScriptGate(res, loadDesignSystems(workingDir, p.File))
		if p.Code != "" {
			if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(srcPath, []byte(p.Code), 0o644); err != nil {
				return "", err
			}
		}
		if len(blocking) > 0 {
			return designScriptRejected(workingDir, srcPath, res, blocking, warnings), nil
		}
		_, err = store.SaveWith(p.File, res.Doc, rev, create, design.SaveOptions{Agent: true, Label: "script " + script})
		if errors.Is(err, design.ErrConflict) && attempt < designScriptRetries {
			continue
		}
		if err != nil {
			return "", err
		}
		res.System.Script = filepath.ToSlash(relTo(workingDir, srcPath))
		res.System.Ran = time.Now().UTC().Format(time.RFC3339)
		data, err := json.MarshalIndent(res.System, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(sysPath), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(sysPath, data, 0o644); err != nil {
			return "", err
		}
		return designScriptReport(workingDir, p.File, srcPath, res, warnings), nil
	}
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

// designScriptGate splits the checks of what a script made into errors,
// which stop the save, and warnings.
func designScriptGate(res *design.ScriptResult, saved []*design.System) (blocking, warnings []design.Issue) {
	all := append([]*design.System{}, saved...)
	sys := res.System
	all = append(all, &sys)
	issues := design.SystemLint(res.Doc, mergedSystem(all), "")
	owned := res.System.Prefix + ":"
	for _, p := range res.Doc.Pages {
		mine := false
		for _, n := range p.Children {
			mine = mine || strings.HasPrefix(n.ID, owned)
		}
		if !mine {
			continue
		}
		report, err := design.Lint(res.Doc, p.ID, "")
		if err != nil {
			continue
		}
		for _, is := range report.Issues {
			if strings.HasPrefix(is.NodeID, owned) {
				issues = append(issues, is)
			}
		}
	}
	for _, is := range issues {
		// Only what this script made can stop it; the rest is reported.
		if is.Severity == "error" && (is.NodeID == "" || strings.HasPrefix(is.NodeID, owned)) {
			blocking = append(blocking, is)
		} else if is.Severity != "info" {
			warnings = append(warnings, is)
		}
	}
	return blocking, warnings
}

func issueLines(b *strings.Builder, list []design.Issue, max int) {
	for i, is := range list {
		if i == max {
			fmt.Fprintf(b, "… and %d more\n", len(list)-i)
			return
		}
		where := ""
		if is.NodeID != "" {
			where = " (" + is.NodeID + ")"
		}
		fmt.Fprintf(b, "- [%s %s]%s %s\n", is.Severity, is.Rule, where, is.Message)
	}
}

func designScriptRejected(workingDir, srcPath string, res *design.ScriptResult, blocking, warnings []design.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NOT SAVED: the script ran (%d edits) but what it made fails %d check(s). The design file is unchanged; the script is saved at %s. Fix the script and run action script again (without code it reruns the saved one).\n", res.Ops, len(blocking), relTo(workingDir, srcPath))
	issueLines(&b, blocking, 25)
	if len(warnings) > 0 {
		b.WriteString("Also worth fixing:\n")
		issueLines(&b, warnings, 10)
	}
	return b.String()
}

func designScriptReport(workingDir, file, srcPath string, res *design.ScriptResult, warnings []design.Issue) string {
	var b strings.Builder
	s := res.System
	sets := map[string]int{}
	for _, c := range s.Components {
		sets[c.Name]++
	}
	names := make([]string, 0, len(sets))
	for n, count := range sets {
		if count > 1 {
			names = append(names, fmt.Sprintf("%s (%d variants)", n, count))
		} else {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	fmt.Fprintf(&b, "Ran %s on %s: %d edits applied as one step; the studio and the Design tab reload it.\n", relTo(workingDir, srcPath), file, res.Ops)
	fmt.Fprintf(&b, "System: %d colour tokens, %d text styles, %d shadows, spacing %v, %d radii.\n", len(s.Colors), len(s.Type), len(s.Shadows), s.Space, len(s.Radius))
	if len(names) > 0 {
		fmt.Fprintf(&b, "Components: %s.\n", strings.Join(names, ", "))
	}
	if len(s.Screens) > 0 {
		screens := make([]string, len(s.Screens))
		for i, sc := range s.Screens {
			screens[i] = fmt.Sprintf("%s (%s)", sc.Name, sc.ID)
		}
		fmt.Fprintf(&b, "Screens: %s.\n", strings.Join(screens, ", "))
	}
	if len(res.Replaced) > 0 {
		fmt.Fprintf(&b, "Replaced what the last run made: %d layer(s).\n", len(res.Replaced))
	}
	if len(res.Removed) > 0 {
		fmt.Fprintf(&b, "Removed, no longer made by the script: %s.\n", strings.Join(res.Removed, ", "))
	}
	if len(res.Kept) > 0 {
		fmt.Fprintf(&b, "Kept because they were edited by hand since the last run (the script's version of them was not applied; pass force true to overwrite): %s.\n", strings.Join(res.Kept, ", "))
	}
	for _, n := range res.Notes {
		b.WriteString("Note: " + n + "\n")
	}
	if len(warnings) > 0 {
		fmt.Fprintf(&b, "Checks passed with %d warning(s); fix them in the script where they are real:\n", len(warnings))
		issueLines(&b, warnings, 15)
	} else {
		b.WriteString("Checks passed: components on auto layout, every variant present, text in styles, colours from tokens.\n")
	}
	b.WriteString("Layers the script made have ids starting with \"" + s.Prefix + ":\" and belong to it: change them in the script and run again. Compose screens from instances of these components (by hand in the studio, or in the script), then action progress shows the next phase.")
	return b.String()
}
