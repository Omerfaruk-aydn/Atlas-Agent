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
	designOutlineLines = 400
	designTouchedLines = 60
	designMaxOps       = 500
)

// DesignCanvasOp is one edit, as the model writes it.
type DesignCanvasOp struct {
	Op         string         `json:"op" description:"add, update, delete, move, make_component, add_page, update_page, delete_page, set_style, delete_style, set_variable, delete_variable, set_modes, add_comment, reply_comment, resolve_comment or delete_comment."`
	ID         string         `json:"id,omitempty" description:"update, delete, move, make_component: the node's id. set_style, delete_style, set_variable, delete_variable: the style or variable id (set_* makes one when empty)."`
	Parent     string         `json:"parent,omitempty" description:"add, move: id of the frame, group or component to put the node in; empty is the page."`
	Index      *int           `json:"index,omitempty" description:"add, move: position among the parent's children, 0 = back; omit for front."`
	Node       map[string]any `json:"node,omitempty" description:"add: the node, children nested inside. Give your own short ids to refer to nodes in later ops."`
	Props      map[string]any `json:"props,omitempty" description:"update: fields to change (x, y, width, height, fills, text, font, ...); font merges; null removes a field."`
	Page       string         `json:"page,omitempty" description:"Page id or name the op works on; defaults to the call's page."`
	Name       string         `json:"name,omitempty" description:"add_page, update_page: page name."`
	Background string         `json:"background,omitempty" description:"add_page, update_page: page background #RRGGBB."`
	Kind       string         `json:"kind,omitempty" description:"set_style, delete_style: fill, text or effect."`
	Modes      []string       `json:"modes,omitempty" description:"set_modes: the variable modes, e.g. [\"Light\",\"Dark\"]."`
	Mode       string         `json:"mode,omitempty" description:"set_modes: the active mode."`
	Text       string         `json:"text,omitempty" description:"add_comment, reply_comment: the message."`
	Author     string         `json:"author,omitempty" description:"add_comment, reply_comment: who writes it; default agent."`
	NodeID     string         `json:"node_id,omitempty" description:"add_comment: pin the comment to this node."`
	X          *float64       `json:"x,omitempty" description:"add_comment: page x of the pin."`
	Y          *float64       `json:"y,omitempty" description:"add_comment: page y of the pin."`
	Resolved   *bool          `json:"resolved,omitempty" description:"resolve_comment: false reopens; default true."`
}

// DesignCanvasParams is the input of design_canvas.
type DesignCanvasParams struct {
	Action string           `json:"action" description:"list, read, create, apply, export or code."`
	File   string           `json:"file,omitempty" description:"Design file name (lower case, digits, '-', '_', '.'), stored as .atlas/design/files/<file>.json. Required except for list."`
	Title  string           `json:"title,omitempty" description:"create: the document's title; the file name is made from it when file is empty."`
	Page   string           `json:"page,omitempty" description:"Page id or name; default the first page."`
	NodeID string           `json:"node_id,omitempty" description:"read: return this node's full JSON. export: export only this node."`
	Format string           `json:"format,omitempty" description:"code: html (default) or react."`
	Ops    []DesignCanvasOp `json:"ops,omitempty" description:"create, apply: edits, applied in order, all or none."`
}

// NewDesignCanvasTool builds design_canvas for the project at workingDir.
func NewDesignCanvasTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		DesignCanvasToolName,
		designCanvasDescription,
		func(ctx context.Context, params DesignCanvasParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if err := ctx.Err(); err != nil {
				return fantasy.ToolResponse{}, err
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
		o := design.Op{Op: op.Op, ID: op.ID, Parent: op.Parent, Index: op.Index, Page: op.Page, Name: op.Name, Background: op.Background, Kind: op.Kind, Modes: op.Modes, Mode: op.Mode, Text: op.Text, Author: op.Author, NodeID: op.NodeID, X: op.X, Y: op.Y, Resolved: op.Resolved}
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
	case "read", "apply", "export", "code":
	default:
		return "", fmt.Errorf("unknown action %q: use list, read, create, apply, export or code", p.Action)
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
		next, res, err := design.Apply(doc, page.ID, ops)
		if err != nil {
			return "", err
		}
		if _, err := store.SaveWith(p.File, next, rev, false, design.SaveOptions{Agent: true, Label: designLabel(p.Ops)}); err != nil {
			if errors.Is(err, design.ErrConflict) {
				return "", fmt.Errorf("the person changed %s while this ran; read it again and redo the edit", p.File)
			}
			return "", err
		}
		return applyReport("Applied", p.File, next, res, len(ops)), nil
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
		return fmt.Sprintf("Exported %s to %s (%d bytes). To look at it, open file:///%s with the browser tool and take a screenshot; text there is laid out by estimate, the Design tab shows real fonts.", base, rel, len(svg), filepath.ToSlash(abs)), nil
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
