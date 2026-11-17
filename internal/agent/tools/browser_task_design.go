package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// designFileName matches the names design_canvas stores files under.
var designFileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)

// validDesignRequirement checks a "design" requirement: its subject names
// the design file and its expect lists the terms the saved file must show.
func validDesignRequirement(r BrowserRequirementInput) error {
	if !designFileName.MatchString(strings.TrimSpace(r.Subject)) {
		return errors.New("design requires subject: the design file name (lower case, digits, '-', '_', '.')")
	}
	for _, term := range strings.Split(r.Expect, ";") {
		if phase, ok := strings.CutPrefix(strings.TrimSpace(term), "@"); ok && !slices.Contains(designPhaseKeys, strings.ToLower(strings.TrimSpace(phase))) {
			return fmt.Errorf("design expect term %q: a phase is one of @%s", term, strings.Join(designPhaseKeys, ", @"))
		}
	}
	if strings.TrimSpace(r.Expect) == "" {
		return errors.New("design requires expect: terms the saved design file must show, separated by ';' (\"Parent > Child\" needs a layer named Child inside the layer named Parent)")
	}
	return nil
}

// designRequirementMet reports whether the saved design file satisfies a
// "design" requirement. The editor autosaves, so the file is the record of
// what was drawn by hand; page evidence goes stale with every edit, this
// does not. Every term must hold: a plain term is text the file contains,
// "A > B" is a layer named B somewhere inside a layer named A, and "@phase"
// (@brief, @system, @components, @screens, @review) is that design phase
// and every one before it done, as action progress reports it.
func designRequirementMet(root string, r browserRequirement) bool {
	if root == "" || !designFileName.MatchString(r.Subject) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, ".atlas", "design", "files", r.Subject+".json"))
	if err != nil || len(data) == 0 {
		return false
	}
	var doc struct {
		Pages []struct {
			Children []designLayer `json:"children"`
		} `json:"pages"`
	}
	parsed := json.Unmarshal(data, &doc) == nil
	text := strings.ToLower(string(data))
	for _, term := range strings.Split(r.Expect, ";") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if phase, ok := strings.CutPrefix(term, "@"); ok {
			if !designPhaseMet(root, r.Subject, strings.ToLower(strings.TrimSpace(phase))) {
				return false
			}
			continue
		}
		if parent, child, nested := strings.Cut(term, ">"); nested {
			if !parsed {
				return false
			}
			found := false
			for _, p := range doc.Pages {
				if layerInside(p.Children, strings.TrimSpace(parent), strings.TrimSpace(child)) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
			continue
		}
		if !strings.Contains(text, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

type designLayer struct {
	Name     string        `json:"name"`
	Children []designLayer `json:"children"`
}

func layerInside(layers []designLayer, parent, child string) bool {
	for _, l := range layers {
		if strings.EqualFold(l.Name, parent) && hasLayer(l.Children, child) {
			return true
		}
		if layerInside(l.Children, parent, child) {
			return true
		}
	}
	return false
}

func hasLayer(layers []designLayer, name string) bool {
	for _, l := range layers {
		if strings.EqualFold(l.Name, name) || hasLayer(l.Children, name) {
			return true
		}
	}
	return false
}
