package tools

import (
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/designstudio"
)

// designStudio opens the design editor for the project on a loopback
// address, for the browser tool to work by mouse and keyboard. The file is
// created empty when it does not exist yet; the person sees every edit live
// in the app's Design tab, which holds the same file.
func designStudio(workingDir string, store *design.Store, p DesignCanvasParams) (string, error) {
	name := p.File
	if name == "" && strings.TrimSpace(p.Title) != "" {
		name = design.Slug(p.Title)
	}
	if name == "" {
		return "", fmt.Errorf("file is required: the design's name (lower case letters, digits, '-', '_', '.'), or a title")
	}
	if !design.ValidName(name) {
		return "", fmt.Errorf("invalid file %q: use lower case letters, digits, '-', '_' and '.'", name)
	}
	created := false
	if _, err := store.Stat(name); err != nil {
		title := p.Title
		if title == "" {
			title = name
		}
		if _, err := store.Save(name, design.New(title), "", true); err != nil {
			return "", err
		}
		created = true
	}
	srv, err := designstudio.Open(workingDir)
	if err != nil {
		return "", err
	}
	state := "Opened"
	if created {
		state = "Created an empty design and opened"
	}
	return fmt.Sprintf("%s %s in the design studio:\n\n  %s\n\n%s", state, name, srv.URL(name), studioGuide), nil
}
