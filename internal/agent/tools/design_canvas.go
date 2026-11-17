package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
)

//go:embed design_canvas.md
var designCanvasDescription string

// DesignCanvasToolName is the tool that reads and edits design files.
const DesignCanvasToolName = "design_canvas"

const (
	// designApplyRetries is how often apply redoes its ops after losing a save race.
	designApplyRetries = 8
	designOutlineLines = 400
	designTouchedLines = 60
	designMaxOps       = 500
)

// DesignCanvasOp is one edit, as the model writes it.
type DesignCanvasOp struct {
	Op         string         `json:"op" description:"set_theme, insert, add, update, delete, move, make_component, add_page, update_page, delete_page, set_style, delete_style, set_variable, delete_variable, set_modes, add_comment, reply_comment, resolve_comment or delete_comment."`
	ID         string         `json:"id,omitempty" description:"insert: id for the block's root (its layers' ids start with it). update, delete, move, make_component: the node's id. set_style, delete_style, set_variable, delete_variable: the style or variable id (set_* makes one when empty)."`
	Block      string         `json:"block,omitempty" description:"insert: the block to place, e.g. web/hero, ios/list, material/top-app-bar (action blocks lists them with their slots)."`
	Accent     string         `json:"accent,omitempty" description:"set_theme: switch the accent to a Radix hue (blue, indigo, violet, crimson, grass, orange...)."`
	Color      string         `json:"color,omitempty" description:"set_theme: derive a custom theme from this brand colour, #RRGGBB."`
	Parent     string         `json:"parent,omitempty" description:"add, insert, move: id of the frame, group or component to put the node in; empty is the page."`
	Index      *int           `json:"index,omitempty" description:"add, insert, move: position among the parent's children, 0 = back; omit for front."`
	Node       map[string]any `json:"node,omitempty" description:"add: the node, children nested inside. Give your own short ids to refer to nodes in later ops."`
	Props      map[string]any `json:"props,omitempty" description:"update: fields to change (x, y, width, height, fills, text, font, ...); font merges; null removes a field. insert: the block's slots (texts and lists), e.g. {title, lead, cta, features:[{icon,title,text}]}."`
	Page       string         `json:"page,omitempty" description:"Page id or name the op works on; defaults to the call's page."`
	Name       string         `json:"name,omitempty" description:"set_theme: the theme id. add_page, update_page: page name."`
	Background string         `json:"background,omitempty" description:"add_page, update_page: page background #RRGGBB."`
	Kind       string         `json:"kind,omitempty" description:"set_style, delete_style: fill, text or effect."`
	Modes      []string       `json:"modes,omitempty" description:"set_modes: the variable modes, e.g. [\"Light\",\"Dark\"]."`
	Mode       string         `json:"mode,omitempty" description:"set_modes, set_theme: the active mode (Light or Dark)."`
	Text       string         `json:"text,omitempty" description:"add_comment, reply_comment: the message."`
	Author     string         `json:"author,omitempty" description:"add_comment, reply_comment: who writes it; default agent."`
	NodeID     string         `json:"node_id,omitempty" description:"add_comment: pin the comment to this node."`
	X          *float64       `json:"x,omitempty" description:"insert: x on the page or in a parent without layout. add_comment: page x of the pin."`
	Y          *float64       `json:"y,omitempty" description:"insert: y on the page or in a parent without layout. add_comment: page y of the pin."`
	Resolved   *bool          `json:"resolved,omitempty" description:"resolve_comment: false reopens; default true."`
}

// DesignCanvasParams is the input of design_canvas.
type DesignCanvasParams struct {
	Action   string           `json:"action" description:"studio (open the design editor in the browser and design in it by mouse and keyboard, like a person), progress (which phase the design is in and what is missing, read from the files), script (run a design script that builds the system and components), references (store screenshots of shipped apps for query, kept with file), compare (the render of file beside its references, with measured questions), list, read, create, apply, export, render, critique, autofix, code, import, themes, blocks, icons, lint, image (an illustration or artwork for file drawn by the image model, saved in its assets), photo (a real licensed photograph for file, saved in its assets), knowledge (platform rules, UX and chart lookup tables, type scales)."`
	File     string           `json:"file,omitempty" description:"Design file name (lower case, digits, '-', '_', '.'), stored as .atlas/design/files/<file>.json. Required except for list."`
	Title    string           `json:"title,omitempty" description:"create: the document's title; the file name is made from it when file is empty."`
	Page     string           `json:"page,omitempty" description:"Page id or name; default the first page."`
	NodeID   string           `json:"node_id,omitempty" description:"read: return this node's full JSON. export: export only this node."`
	Format   string           `json:"format,omitempty" description:"code: html (default) or react."`
	Source   string           `json:"source,omitempty" description:"import: HTML (with inline styles or <style> rules) to turn into design nodes, typically the rendered HTML of a running web app."`
	Path     string           `json:"path,omitempty" description:"import: an HTML file in the project to import instead of source."`
	Width    float64          `json:"width,omitempty" description:"import: the width of the imported frame; default 1280."`
	Query    string           `json:"query,omitempty" description:"icons: words to search icon names and tags for, e.g. \"shopping cart\". blocks: web, ios or android to list one platform's blocks. references: a kind of app, or app names separated by ;. photo: what the photograph shows. knowledge: words to search the tables for."`
	Prompt   string           `json:"prompt,omitempty" description:"image: what to draw, in plain words. Start with the file's one style sentence (medium, light, palette), then the subject; ask for no text in the picture."`
	Image    string           `json:"image,omitempty" description:"image, photo: the file name to save as, e.g. tile-schedule. The result gives the src to place it with (assets/<name>.png)."`
	Aspect   string           `json:"aspect,omitempty" description:"image, photo: square (default), portrait, tall, landscape or wide."`
	Platform string           `json:"platform,omitempty" description:"references: android searches Google Play instead of the App Store. knowledge topic platform: ios (default), android, tablet or web."`
	Topic    string           `json:"topic,omitempty" description:"knowledge: platform (the written rules of the platform), ux, mobile or charts (search the lookup tables with query), scale (a type scale from the scale words)."`
	Scale    string           `json:"scale,omitempty" description:"knowledge topic scale: key=value words, e.g. base=17 ratio=1.2."`
	Pick     int              `json:"pick,omitempty" description:"photo: which search result to use (1 is the best match; try 2 or 3 when the first is wrong)."`
	Code     string           `json:"code,omitempty" description:"script: the design script, JavaScript (see atlas://skills/design-script/SKILL.md). It is saved as .atlas/design/scripts/<file>.js; omit it to run the saved script again."`
	Script   string           `json:"script,omitempty" description:"script: a name for a second script on the same file (e.g. screens); its layers get their own id prefix. Default: the file name."`
	Force    bool             `json:"force,omitempty" description:"script: also replace layers the script made that were edited by hand since its last run."`
	Ops      []DesignCanvasOp `json:"ops,omitempty" description:"create, apply: edits, applied in order, all or none."`
}

// ImageGenerator makes one picture from a prompt through whatever model the
// person is signed in to. aspect is a ratio such as "16:9". The data is a
// PNG, JPEG or WebP and mime says which.
type ImageGenerator func(ctx context.Context, prompt, aspect string) (data []byte, mime string, err error)

// NewDesignCanvasTool builds design_canvas for the project at workingDir. An
// optional generator turns on the image action.
func NewDesignCanvasTool(workingDir string, gen ...ImageGenerator) fantasy.AgentTool {
	var generate ImageGenerator
	if len(gen) > 0 {
		generate = gen[0]
	}
	return fantasy.NewAgentTool(
		DesignCanvasToolName,
		designCanvasDescription,
		func(ctx context.Context, params DesignCanvasParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
			}
			if params.Action == "image" {
				return designImage(ctx, workingDir, generate, params), nil
			}
			if params.Action == "script" {
				text, err := designScript(ctx, workingDir, design.NewStore(workingDir), params)
				if err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				return fantasy.NewTextResponse(text), nil
			}
			if params.Action == "references" {
				return designReferences(ctx, workingDir, params), nil
			}
			if params.Action == "compare" {
				return designCompareAction(workingDir, params), nil
			}
			if params.Action == "photo" {
				return designPhoto(ctx, workingDir, params), nil
			}
			store := design.NewStore(workingDir)
			text, err := runDesignCanvas(workingDir, store, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(text), nil
		},
	)
}

func designOps(in []DesignCanvasOp) ([]design.Op, error) {
	if len(in) > designMaxOps {
		return nil, fmt.Errorf("at most %d ops per call; split the work into several calls", designMaxOps)
	}
	out := make([]design.Op, len(in))
	for i, op := range in {
		o := design.Op{Op: op.Op, ID: op.ID, Parent: op.Parent, Index: op.Index, Page: op.Page, Name: op.Name, Background: op.Background, Kind: op.Kind, Modes: op.Modes, Mode: op.Mode, Text: op.Text, Author: op.Author, NodeID: op.NodeID, X: op.X, Y: op.Y, Resolved: op.Resolved, Block: op.Block, Accent: op.Accent, Color: op.Color}
		if op.Node != nil {
			raw, err := json.Marshal(op.Node)
			if err != nil {
				return nil, fmt.Errorf("op %d: node: %w", i, err)
			}
			o.Node = raw
		}
		if op.Props != nil {
			o.Props = map[string]json.RawMessage{}
			for k, v := range op.Props {
				raw, err := json.Marshal(v)
				if err != nil {
					return nil, fmt.Errorf("op %d: props.%s: %w", i, k, err)
				}
				o.Props[k] = raw
			}
		}
		out[i] = o
	}
	return out, nil
}

func runDesignCanvas(workingDir string, store *design.Store, p DesignCanvasParams) (string, error) {
	switch p.Action {
	case "list":
		files, err := store.List()
		if err != nil {
			return "", err
		}
		if len(files) == 0 {
			return "No design files yet. Use action \"create\" to start one; the person sees it in the Design tab of the Atlas app.", nil
		}
		var b strings.Builder
		b.WriteString("Design files (newest first):\n")
		for _, f := range files {
			fmt.Fprintf(&b, "- %s (%s, %d bytes)\n", f.Name, design.RelPath(f.Name), f.Size)
		}
		return b.String(), nil
	case "create":
		name := p.File
		if name == "" {
			name = design.Slug(p.Title)
		}
		if !design.ValidName(name) {
			return "", fmt.Errorf("invalid file %q: use lower case letters, digits, '-', '_' and '.'", name)
		}
		title := p.Title
		if title == "" {
			title = name
		}
		doc := design.New(title)
		ops, err := designOps(p.Ops)
		if err != nil {
			return "", err
		}
		next, res, err := design.Apply(doc, "", ops)
		if err != nil {
			return "", err
		}
		if _, err := store.Save(name, next, "", true); err != nil {
			return "", err
		}
		return applyReport("Created", name, next, res, len(ops)), nil
	case "import":
		return designImport(workingDir, store, p)
	case "themes":
		return designThemes(), nil
	case "blocks":
		return designBlocks(p.Query), nil
	case "icons":
		return designIcons(p.Query)
	case "knowledge":
		return designKnowledge(p)
	case "studio":
		return designStudio(workingDir, store, p)
	case "progress":
		return designProgressText(workingDir, store, p)
	case "read", "apply", "export", "render", "critique", "autofix", "code", "lint":
	default:
		return "", fmt.Errorf("unknown action %q: use studio, progress, script, references, compare, list, read, create, apply, export, render, critique, autofix, code, import, themes, blocks, icons, lint, knowledge, image or photo", p.Action)
	}
	if !design.ValidName(p.File) {
		return "", fmt.Errorf("file is required: the design's name, as action \"list\" shows it")
	}
	doc, rev, err := store.Load(p.File)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no design named %q; action \"list\" shows the designs, \"create\" makes one", p.File)
	}
	if err != nil {
		return "", err
	}
	page := doc.PageByRef(p.Page)
	if page == nil {
		return "", fmt.Errorf("no page %q in %s", p.Page, p.File)
	}
	switch p.Action {
	case "lint":
		report, err := design.Lint(doc, page.ID, p.NodeID)
		if err != nil {
			return "", err
		}
		report = report.Merge(design.SystemLint(doc, mergedSystem(loadDesignSystems(workingDir, p.File)), page.ID))
		report = report.WithRenderCheck(doc, page.ID)
		return report.Text(40) + "Fix every error and the warnings that are not deliberate, then lint again; a finished design scores 90 or more. Render and critique first: lint fails a page that has not been rendered since it changed.", nil
	case "render":
		return designRender(workingDir, store, doc, rev, p.File, page, p.NodeID)
	case "critique":
		return designCritique(workingDir, doc, p.File, page, p.NodeID)
	case "autofix":
		ops, err := design.Autofix(doc, page.ID, p.NodeID)
		if err != nil {
			return "", err
		}
		if len(ops) == 0 {
			return "No safe fixes. Spacing that is already on an even pixel is left alone.", nil
		}
		next, res, err := design.Apply(doc, page.ID, ops)
		if err != nil {
			return "", err
		}
		if _, err := store.SaveWith(p.File, next, rev, false, design.SaveOptions{Agent: true, Label: "autofix"}); err != nil {
			return "", err
		}
		return applyReport("Fixed", p.File, next, res, len(ops)), nil
	case "read":
		if p.NodeID != "" {
			at := doc.Find(p.NodeID)
			if at == nil {
				return "", fmt.Errorf("no node %q in %s", p.NodeID, p.File)
			}
			data, err := json.MarshalIndent(at.Node, "", " ")
			if err != nil {
				return "", err
			}
			parent := "the page"
			if at.Parent != nil {
				parent = fmt.Sprintf("%s %q (%s)", at.Parent.Type, at.Parent.Name, at.Parent.ID)
			}
			return fmt.Sprintf("Node %s on page %q, inside %s, at index %d:\n%s", p.NodeID, at.Page.Name, parent, at.Index, data), nil
		}
		return fmt.Sprintf("%s (%s)\n%s", p.File, design.RelPath(p.File), design.Outline(doc, designOutlineLines)), nil
	case "apply":
		if len(p.Ops) == 0 {
			return "", fmt.Errorf("apply needs ops")
		}
		ops, err := designOps(p.Ops)
		if err != nil {
			return "", err
		}
		// Several agents (a swarm) can write the same file, each on its own page: when
		// another save got in first, the ops are applied again on what it left.
		for attempt := 0; ; attempt++ {
			next, res, err := design.Apply(doc, page.ID, ops)
			if err != nil {
				return "", err
			}
			_, err = store.SaveWith(p.File, next, rev, false, design.SaveOptions{Agent: true, Label: designLabel(p.Ops)})
			if err == nil {
				return applyReport("Applied", p.File, next, res, len(ops)), nil
			}
			if !errors.Is(err, design.ErrConflict) {
				return "", err
			}
			if attempt == designApplyRetries {
				return "", fmt.Errorf("%s keeps changing under this edit (another agent or the person is saving); read it again and redo the edit", p.File)
			}
			doc, rev, err = store.Load(p.File)
			if err != nil {
				return "", err
			}
			if page = doc.PageByRef(page.ID); page == nil {
				return "", fmt.Errorf("page %q was deleted while this ran", p.Page)
			}
		}
	case "code":
		if p.NodeID == "" {
			return "", fmt.Errorf("code needs node_id: the frame to turn into code")
		}
		react := p.Format == "react"
		out, err := design.GenerateCode(doc, p.NodeID, react)
		if err != nil {
			return "", err
		}
		ext := ".html"
		if react {
			ext = ".jsx"
		}
		name := design.Slug(p.File + "-" + p.NodeID)
		dir := filepath.Join(workingDir, ".atlas", "design", "exports")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		markup := out.Markup
		if !react {
			markup = "<!doctype html>\n<meta charset=\"utf-8\">\n<link rel=\"stylesheet\" href=\"" + name + ".css\">\n" + markup + "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name+ext), []byte(markup), 0o644); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".css"), []byte(out.CSS), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Wrote .atlas/design/exports/%s%s and %s.css (%d + %d bytes). Flexbox is used where a frame has auto layout, absolute positions elsewhere; images keep their src. Move the files into the project and adapt names and fonts.", name, ext, name, len(markup), len(out.CSS)), nil
	default: // export
		var ids []string
		if p.NodeID != "" {
			ids = []string{p.NodeID}
		}
		svg, err := design.SVG(doc, page, ids)
		if err != nil {
			return "", err
		}
		base := p.File
		if p.NodeID != "" {
			base += "-" + p.NodeID
		} else if len(doc.Pages) > 1 {
			base += "-" + design.Slug(page.Name)
		}
		rel := filepath.ToSlash(filepath.Join(".atlas", "design", "exports", base+".svg"))
		abs := filepath.Join(workingDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(abs, []byte(svg), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("Exported %s to %s (%d bytes). Action render writes a PNG of this with the kit fonts; critique scores that picture.", base, rel, len(svg)), nil
	}
}

func applyReport(verb, name string, doc *design.Doc, res design.Result, ops int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d op(s) to %s (%s). The Design tab shows it live.\n", verb, ops, name, design.RelPath(name))
	if len(res.Created) > 0 {
		keys := make([]int, 0, len(res.Created))
		for k := range res.Created {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		b.WriteString("Created:")
		for _, k := range keys {
			fmt.Fprintf(&b, " op%d→%s", k, res.Created[k])
		}
		b.WriteByte('\n')
	}
	for _, n := range res.Notes {
		b.WriteString("Note: " + n + "\n")
	}
	if len(res.Touched) > 0 {
		b.WriteString("Changed nodes:\n")
		for i, id := range res.Touched {
			if i == designTouchedLines {
				fmt.Fprintf(&b, "… and %d more\n", len(res.Touched)-i)
				break
			}
			if at := doc.Find(id); at != nil {
				b.WriteString("- " + design.Describe(at.Node) + "\n")
			}
		}
	}
	return b.String()
}

// designLabel says what an agent edit was, for the history of the file.
func designLabel(ops []DesignCanvasOp) string {
	return fmt.Sprintf("agent %d edits", len(ops))
}

// designImport turns HTML into a frame on the design's page: the way a running app's UI, in any web framework, becomes a design.
func designImport(workingDir string, store *design.Store, p DesignCanvasParams) (string, error) {
	if !design.ValidName(p.File) {
		return "", fmt.Errorf("file is required: the design to import into (it is created when it does not exist)")
	}
	src := p.Source
	if src == "" && p.Path != "" {
		abs := p.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workingDir, abs)
		}
		rel, err := filepath.Rel(workingDir, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path must be inside the project")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		src = string(data)
	}
	if strings.TrimSpace(src) == "" {
		return "", fmt.Errorf("import needs source (HTML text) or path (an HTML file in the project)")
	}
	root, err := design.ImportHTML(src, design.ImportOptions{Width: p.Width})
	if err != nil {
		return "", err
	}
	doc, rev, err := store.Load(p.File)
	create := errors.Is(err, os.ErrNotExist)
	if create {
		doc, rev, err = design.New(root.Name), "", nil
	}
	if err != nil {
		return "", err
	}
	page := doc.PageByRef(p.Page)
	if page == nil {
		return "", fmt.Errorf("no page %q in %s", p.Page, p.File)
	}
	// Land to the right of what is already there.
	for _, n := range page.Children {
		if edge := n.X + n.Width + 100; edge > root.X {
			root.X = edge
		}
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	next, res, err := design.Apply(doc, page.ID, []design.Op{{Op: "add", Node: raw}})
	if err != nil {
		return "", err
	}
	if _, err := store.SaveWith(p.File, next, rev, create, design.SaveOptions{Agent: true, Label: "Import HTML"}); err != nil {
		if errors.Is(err, design.ErrConflict) {
			return "", fmt.Errorf("the person changed %s while this ran; import again", p.File)
		}
		return "", err
	}
	return applyReport("Imported", p.File, next, res, 1) + " Flexbox became auto layout and block flow a vertical stack; margins are not modelled, remote images became grey placeholders and svg/canvas/video became rectangles. Check the result with the export action and fix what differs.", nil
}

// designThemes lists the kit's themes with what sets each apart.
func designThemes() string {
	var b strings.Builder
	b.WriteString("Themes (set one with {op:\"set_theme\", name, accent?, mode?}; every block and the c-* colour variables, t-* text styles and e-* shadows follow it):\n")
	for _, t := range design.Themes() {
		fmt.Fprintf(&b, "- %s (%s, %s): %s Fonts %s / %s; radius %g/%g/%g/%g. Source: %s.\n", t.ID, t.Name, t.Platform, t.Note, t.Fonts.Heading, t.Fonts.Body, t.Radius.SM, t.Radius.MD, t.Radius.LG, t.Radius.XL, t.Source)
	}
	fmt.Fprintf(&b, "Accents (Radix hues) for any theme: %s.\n", strings.Join(design.Accents(), ", "))
	b.WriteString("Colour tokens (variable ids c-<token>): " + strings.Join(design.ColorTokens, ", ") + ".\n")
	b.WriteString("Type steps (text style ids t-<step>): " + strings.Join(design.TypeSlots, ", ") + ".\n")
	b.WriteString("Shadows (effect style ids): e-sm, e-md, e-lg.")
	return b.String()
}

// designBlocks lists the kit's blocks, one platform or all.
func designBlocks(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "material" {
		platform = "android"
	}
	list := design.Blocks(platform)
	if len(list) == 0 {
		return "No blocks for platform " + platform + "; use web, ios or android."
	}
	var b strings.Builder
	b.WriteString("Blocks (place with {op:\"insert\", block, id?, parent?, index?, x?, y?, props:{slot: value}}; slots you leave out keep the sample copy shown, so replace all of it with the brief's own words):\n")
	for _, bl := range list {
		fmt.Fprintf(&b, "- %s: %s. %s (%.0fx%.0f). Slots: ", bl.ID, bl.Title, bl.Description, bl.Width, bl.Height)
		keys := make([]string, 0, len(bl.Slots))
		for k := range bl.Slots {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteString("; ")
			}
			data, _ := json.Marshal(bl.Slots[k])
			v := string(data)
			if len(v) > 160 {
				v = v[:157] + "..."
			}
			fmt.Fprintf(&b, "%s=%s", k, v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// designIcons searches the icon set.
func designIcons(query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("icons needs query: words such as \"cart\" or \"arrow right\"")
	}
	found := design.SearchIcons(query, 30)
	if len(found) == 0 {
		return fmt.Sprintf("No icon matches %q among %d; try a simpler word (e.g. \"user\", \"chart\", \"settings\").", query, design.IconCount()), nil
	}
	names := make([]string, len(found))
	for i, ic := range found {
		names[i] = ic.Name
	}
	return fmt.Sprintf("Icons for %q (Lucide, %d in all): %s.\nPlace one with an add op node {\"type\":\"path\",\"icon\":\"<name>\",\"width\":20} (stroked in the theme's text colour; set strokes to recolour), or pass it to a block slot named icon.", query, design.IconCount(), strings.Join(names, ", ")), nil
}
