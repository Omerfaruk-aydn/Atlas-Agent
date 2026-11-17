package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

// A design moves through fixed phases. Each has an exit condition read from
// the files, so the agent knows when a phase is done without a screenshot,
// and a budget of tool calls, so it stops instead of polishing forever.

// designPhase is one phase and whether the files show it done.
type designPhase struct {
	Key     string
	Title   string
	Done    bool
	Missing []string
	Next    string
	Budget  string
}

// designPhaseKeys are the phases in order.
var designPhaseKeys = []string{"brief", "system", "components", "screens", "review"}

const (
	minColourTokens = 4
	minTextStyles   = 4
	minComponents   = 3
	minScreenLayers = 5
	reviewScore     = 90
	briefMinBytes   = 300
	minReferences   = 2
	minReviewRounds = 2
)

// briefReferences counts the lines under the brief's "References" heading:
// the products it learns from. A design with none starts from nothing.
func briefReferences(text string) int {
	in, n := false, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			in = strings.Contains(strings.ToLower(line), "reference")
			continue
		}
		if in && len(strings.Trim(line, "-*0123456789. ")) > 3 {
			n++
		}
	}
	return n
}

func designProgress(workingDir, file string, doc *design.Doc) []designPhase {
	var phases []designPhase

	brief := designPhase{
		Key: "brief", Title: "Brief", Budget: "3 calls",
		Next: "Read atlas://skills/design-taste/SKILL.md and atlas://skills/design-brief/SKILL.md, then write .atlas/design/briefs/" + file + ".md (audience, job of each screen, real content, one visual idea, not-this list).",
	}
	briefPath := filepath.Join(workingDir, ".atlas", "design", "briefs", file+".md")
	if info, err := os.Stat(briefPath); err != nil {
		brief.Missing = append(brief.Missing, "no brief at .atlas/design/briefs/"+file+".md")
	} else if info.Size() < briefMinBytes {
		brief.Missing = append(brief.Missing, fmt.Sprintf("the brief is only %d bytes", info.Size()))
	} else if data, err := os.ReadFile(briefPath); err == nil && !strings.Contains(strings.ToLower(string(data)), "direction") {
		brief.Missing = append(brief.Missing, "the brief has no \"Direction\" section: the look (editorial, swiss, soft, night, civic or your own), the display and body fonts, the shape rule and the colour logic, chosen from the brief's feeling words")
	} else if err == nil && briefReferences(string(data)) < minReferences {
		brief.Missing = append(brief.Missing, fmt.Sprintf("the brief names fewer than %d references (a \"References\" heading with two real products and what to take from each)", minReferences))
	}
	brief.Done = len(brief.Missing) == 0
	phases = append(phases, brief)

	systems := loadDesignSystems(workingDir, file)
	sys := mergedSystem(systems)
	colours, texts := 0, 0
	if doc.Variables != nil {
		colours = len(doc.Variables.Items)
	}
	if doc.Styles != nil {
		texts = len(doc.Styles.Text)
	}
	system := designPhase{
		Key: "system", Title: "System", Budget: "2 calls (one script, one fix)",
		Next: "Write the tokens as a design script (atlas://skills/design-script/SKILL.md): colors() with light and dark, type() with 5-7 roles, space(), radius(), shadow(); run design_canvas action script.",
	}
	if colours < minColourTokens {
		system.Missing = append(system.Missing, fmt.Sprintf("%d colour tokens (need %d+)", colours, minColourTokens))
	}
	if texts < minTextStyles {
		system.Missing = append(system.Missing, fmt.Sprintf("%d text styles (need %d+)", texts, minTextStyles))
	}
	if sys == nil || len(sys.Space) == 0 {
		system.Missing = append(system.Missing, "no spacing scale declared (space([...]) in a script)")
	}
	system.Done = len(system.Missing) == 0
	phases = append(phases, system)

	issues := design.SystemLint(doc, sys, "")
	mains, sets := 0, map[string]bool{}
	for _, p := range doc.Pages {
		design.Walk(p.Children, func(n *design.Node, _ *design.Node, _ int) bool {
			if n.Type == design.TypeComponent {
				if n.Set != "" {
					if !sets[n.Set] {
						mains++
					}
					sets[n.Set] = true
				} else {
					mains++
				}
			}
			return true
		})
	}
	components := designPhase{
		Key: "components", Title: "Components", Budget: "4 calls",
		Next: "Add the components the screens need to the script, each on auto layout with its states as variants (variants()), placed with library(); run it again.",
	}
	if mains < minComponents {
		components.Missing = append(components.Missing, fmt.Sprintf("%d components (need %d+: e.g. button, list row, tab bar, card)", mains, minComponents))
	}
	for _, is := range issues {
		if is.Severity == "error" && (strings.HasPrefix(is.Rule, "component") || is.Rule == "variants") {
			components.Missing = append(components.Missing, is.Message)
		}
	}
	components.Done = len(components.Missing) == 0
	phases = append(phases, components)

	screens := designPhase{
		Key: "screens", Title: "Screens", Budget: "about 25 calls per screen",
		Next: "Compose each screen from instances: in the studio by hand (tool_pipeline browser steps, one screenshot per section), or with screen() in a script for the skeleton, then finish by hand.",
	}
	var screenPages []*design.Page
	count := 0
	for _, p := range doc.Pages {
		has := false
		for _, n := range p.Children {
			if !design.IsScreen(n) {
				continue
			}
			has = true
			count++
			layers, instances := 0, 0
			design.Walk(n.Children, func(c *design.Node, _ *design.Node, _ int) bool {
				layers++
				if c.Type == design.TypeInstance {
					instances++
				}
				return true
			})
			if layers < minScreenLayers {
				screens.Missing = append(screens.Missing, fmt.Sprintf("screen %q has only %d layers", n.Name, layers))
			}
			if instances == 0 && mains > 0 {
				screens.Missing = append(screens.Missing, fmt.Sprintf("screen %q uses no component instances", n.Name))
			}
		}
		if has {
			screenPages = append(screenPages, p)
		}
	}
	if count == 0 {
		screens.Missing = append(screens.Missing, "no screen yet (a top-level frame at a device size)")
	}
	screens.Done = len(screens.Missing) == 0
	phases = append(phases, screens)

	review := designPhase{
		Key: "review", Title: "Review", Budget: "8 calls",
		Next: "Per page with screens: design_canvas render, critique (atlas://skills/design-critique and anti-slop), fix, then lint until it scores " + fmt.Sprint(reviewScore) + "+ with no errors.",
	}
	if len(screenPages) > 0 {
		log, _ := os.ReadFile(filepath.Join(workingDir, ".atlas", "design", "reviews", file+".md"))
		if rounds := strings.Count(strings.ToLower(string(log)), "## round"); rounds < minReviewRounds {
			review.Missing = append(review.Missing, fmt.Sprintf("%d review rounds logged in .atlas/design/reviews/%s.md (need %d: render, hostile critic subagent, list measured flaws and fixes, render again; atlas://skills/design-director/SKILL.md)", rounds, file, minReviewRounds))
		}
		compared, _ := filepath.Glob(filepath.Join(workingDir, ".atlas", "design", "exports", file+"*-compare.png"))
		if len(compared) == 0 || !strings.Contains(strings.ToLower(string(log)), "reference:") {
			review.Missing = append(review.Missing, "no round against shipped work: design_canvas references (file, query: the kind of app or two app names), then compare, and log the measured gaps under a \"Reference: <apps>\" line in a round")
		}
	}
	for _, p := range screenPages {
		if !doc.Rendered(p.ID) {
			review.Missing = append(review.Missing, fmt.Sprintf("page %q not rendered since it changed", p.Name))
			continue
		}
		report, err := design.Lint(doc, p.ID, "")
		if err != nil {
			continue
		}
		report = report.Merge(design.SystemLint(doc, sys, p.ID))
		errs := 0
		for _, is := range report.Issues {
			if is.Severity == "error" {
				errs++
			}
		}
		if report.Score < reviewScore || errs > 0 {
			review.Missing = append(review.Missing, fmt.Sprintf("page %q lints %d/100 with %d error(s)", p.Name, report.Score, errs))
		}
	}
	if len(screenPages) == 0 {
		review.Missing = append(review.Missing, "nothing to review yet")
	}
	review.Done = len(review.Missing) == 0
	phases = append(phases, review)
	return phases
}

func designProgressText(workingDir string, store *design.Store, p DesignCanvasParams) (string, error) {
	if !design.ValidName(p.File) {
		return "", fmt.Errorf("file is required: the design to report on")
	}
	doc, _, err := store.Load(p.File)
	if errors.Is(err, os.ErrNotExist) {
		doc = design.New(p.File)
	} else if err != nil {
		return "", err
	}
	phases := designProgress(workingDir, p.File, doc)
	var b strings.Builder
	fmt.Fprintf(&b, "Progress of %s (read from the files; no screenshot needed):\n", p.File)
	current := -1
	for i, ph := range phases {
		mark := "[x]"
		if !ph.Done {
			mark = "[ ]"
			if current < 0 {
				current = i
			}
		}
		fmt.Fprintf(&b, "%s %d. %s", mark, i+1, ph.Title)
		if !ph.Done {
			b.WriteString(": " + strings.Join(firstN(ph.Missing, 4), "; "))
		}
		b.WriteByte('\n')
	}
	if current < 0 {
		b.WriteString("Every phase is done. Report to the person with the rendered PNGs; stop here unless they ask for more.")
		return b.String(), nil
	}
	ph := phases[current]
	fmt.Fprintf(&b, "\nNow: %s. %s\nBudget: %s for this phase. When it is spent, stop and report what is left instead of starting over.\nWork phase by phase: a later phase built on an unfinished one is redone later.", ph.Title, ph.Next, ph.Budget)
	return b.String(), nil
}

func firstN(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(append([]string{}, list[:n]...), fmt.Sprintf("and %d more", len(list)-n))
}

// designPhaseMet reports whether the design file has reached a phase: the
// phase and every one before it are done.
func designPhaseMet(workingDir, file, key string) bool {
	store := design.NewStore(workingDir)
	doc, _, err := store.Load(file)
	if err != nil {
		return false
	}
	for _, ph := range designProgress(workingDir, file, doc) {
		if !ph.Done {
			return false
		}
		if ph.Key == key {
			return true
		}
	}
	return false
}
