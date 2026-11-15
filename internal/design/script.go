package design

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// A design script is a small JavaScript program that builds a design
// system: colour variables, text and effect styles, the spacing and radius
// scales, components with their variants, and if wanted the skeleton of
// screens made from instances. It runs in a sandbox with no file, network
// or clock access; what it records is applied to the file in one step, and
// running it again replaces what it made before.

//go:embed script_prelude.js
var scriptPrelude string

const (
	// ScriptMaxSource bounds a script's size.
	ScriptMaxSource = 256 << 10
	// ScriptMaxOps bounds what one script may record.
	ScriptMaxOps = 4000
	// scriptTimeout bounds a script's run time.
	scriptTimeout = 5 * time.Second
	// sizePasses bounds the passes that size instances after their mains.
	sizePasses = 4
)

var prefixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)

// ScriptPrefix is the id prefix of the layers a script named name owns.
func ScriptPrefix(name string) string {
	p := Slug(name)
	p = strings.Trim(strings.ReplaceAll(p, ".", "-"), "-_")
	p = strings.ReplaceAll(p, "_", "-")
	if len(p) > 16 {
		p = strings.TrimRight(p[:16], "-")
	}
	if !prefixRe.MatchString(p) {
		p = "sys"
	}
	return p
}

// SystemRef is a component or screen a script made.
type SystemRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SystemColor is a colour token.
type SystemColor struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Value  string            `json:"value"`
	Values map[string]string `json:"values"`
}

// SystemType is a text style.
type SystemType struct {
	ID            string   `json:"id"`
	Font          Font     `json:"font"`
	LineHeight    float64  `json:"line_height"`
	LetterSpacing *float64 `json:"letter_spacing,omitempty"`
	Transform     string   `json:"transform,omitempty"`
}

// System is what a design script declared, kept next to the design so lint
// and progress can hold the file to it.
type System struct {
	Script     string                     `json:"script"`
	Prefix     string                     `json:"prefix"`
	Colors     map[string]SystemColor     `json:"colors"`
	Type       map[string]SystemType      `json:"type"`
	Space      []float64                  `json:"space"`
	Radius     map[string]json.RawMessage `json:"radius"`
	Shadows    map[string]json.RawMessage `json:"shadows"`
	Components []SystemRef                `json:"components"`
	Screens    []SystemRef                `json:"screens"`
	// Roots are the top-level layers the script placed, with the hash of
	// each as the script left it. A root whose hash changed was edited by
	// hand and is kept on the next run unless forced.
	Roots map[string]string `json:"roots"`
	// Ran is when the script last ran, RFC 3339.
	Ran string `json:"ran,omitempty"`
}

// RadiusValues are the radius scale's single values.
func (s *System) RadiusValues() []float64 {
	var out []float64
	for _, raw := range s.Radius {
		var v float64
		if json.Unmarshal(raw, &v) == nil {
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

// ScriptOptions set how a script runs.
type ScriptOptions struct {
	// Name is the script's name; it sets the id prefix of what it makes.
	Name string
	// Force replaces layers the person edited since the last run.
	Force bool
	// Timeout bounds the run; zero is five seconds.
	Timeout time.Duration
}

// ScriptResult is a script's outcome.
type ScriptResult struct {
	Doc    *Doc
	System System
	// Ops is how many edits the script recorded.
	Ops int
	// Replaced are the layers made again; Kept the hand-edited ones left
	// alone; Removed the ones the script no longer makes.
	Replaced, Kept, Removed []string
	Notes                   []string
}

// ScriptError is a script that failed, with the line when it is known.
type ScriptError struct{ Msg string }

func (e *ScriptError) Error() string { return e.Msg }

type scriptOut struct {
	Ops    []json.RawMessage `json:"ops"`
	System struct {
		Colors     map[string]SystemColor     `json:"colors"`
		Type       map[string]SystemType      `json:"type"`
		Space      []float64                  `json:"space"`
		Radius     map[string]json.RawMessage `json:"radius"`
		Shadows    map[string]json.RawMessage `json:"shadows"`
		Components []SystemRef                `json:"components"`
		Screens    []SystemRef                `json:"screens"`
	} `json:"system"`
}

type scriptVar struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// scriptEnv is what the script knows about the file it runs on.
func scriptEnv(d *Doc, name, prefix string) map[string]any {
	pages := make([]string, 0, len(d.Pages))
	for _, p := range d.Pages {
		pages = append(pages, p.Name)
	}
	modes, mode := []string{}, ""
	vars := map[string]scriptVar{}
	if v := d.Variables; v != nil {
		modes, mode = append(modes, v.Modes...), v.Mode
		for id, item := range v.Items {
			value := item.Values[v.Mode]
			if value == "" {
				for _, c := range item.Values {
					value = c
					break
				}
			}
			sv := scriptVar{ID: id, Value: value}
			vars[id] = sv
			if item.Name != "" {
				vars[item.Name] = sv
			}
		}
	}
	return map[string]any{"file": name, "prefix": prefix, "modes": modes, "mode": mode, "pages": pages, "vars": vars}
}

// RunScript runs a design script on d and returns the changed document. prev
// is the system the script's last run left, or nil.
func RunScript(ctx context.Context, d *Doc, prev *System, src string, opts ScriptOptions) (*ScriptResult, error) {
	if len(src) > ScriptMaxSource {
		return nil, fmt.Errorf("the script is %d KB; keep it under %d KB", len(src)>>10, ScriptMaxSource>>10)
	}
	prefix := ScriptPrefix(opts.Name)
	out, err := evalScript(ctx, d, opts, prefix, src)
	if err != nil {
		return nil, err
	}
	if len(out.Ops) > ScriptMaxOps {
		return nil, fmt.Errorf("the script recorded %d edits; at most %d", len(out.Ops), ScriptMaxOps)
	}
	ops := make([]Op, 0, len(out.Ops))
	for i, raw := range out.Ops {
		var op Op
		if err := json.Unmarshal(raw, &op); err != nil {
			return nil, fmt.Errorf("edit %d: %w", i, err)
		}
		ops = append(ops, op)
	}
	res := &ScriptResult{Ops: len(ops)}
	sys := System{
		Prefix: prefix, Colors: out.System.Colors, Type: out.System.Type, Space: out.System.Space,
		Radius: out.System.Radius, Shadows: out.System.Shadows, Components: out.System.Components,
		Screens: out.System.Screens, Roots: map[string]string{},
	}

	// What the script places this time, by root id.
	placed := map[string]bool{}
	auto := map[string]autoAxes{}
	for _, op := range ops {
		if op.Op != "add" || len(op.Node) == 0 {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal(op.Node, &root); err != nil {
			return nil, err
		}
		if id, _ := root["id"].(string); id != "" {
			placed[id] = true
		}
		markAutoInstances(root, auto)
	}

	// Replace what the last run made, except roots edited by hand.
	owned := prefix + ":"
	// Deletes go in a batch of their own: a batch reserves the ids it starts
	// with, so a layer made again under its old id needs the old one gone.
	var drop, first []Op
	keep := map[string]bool{}
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if !strings.HasPrefix(n.ID, owned) {
				return true
			}
			if old, ok := prevRoot(prev, n.ID); ok && !opts.Force && NodeHash(n) != old {
				keep[n.ID] = true
				res.Kept = append(res.Kept, fmt.Sprintf("%s %q", n.ID, n.Name))
				sys.Roots[n.ID] = old
				return false
			}
			drop = append(drop, Op{Op: "delete", ID: n.ID})
			if placed[n.ID] {
				res.Replaced = append(res.Replaced, n.ID)
			} else {
				res.Removed = append(res.Removed, fmt.Sprintf("%s %q", n.ID, n.Name))
			}
			return false
		})
	}
	var later []Op
	for _, op := range ops {
		if op.Op == "add" {
			var head struct {
				ID          string `json:"id"`
				Type        string `json:"type"`
				ComponentID string `json:"component_id"`
			}
			_ = json.Unmarshal(op.Node, &head)
			if keep[head.ID] {
				continue
			}
			// A top-level instance needs its main in the file first.
			if head.Type == string(TypeInstance) {
				later = append(later, op)
				continue
			}
		}
		first = append(first, op)
	}

	doc := d
	for _, batch := range [][]Op{drop, first, later} {
		if len(batch) == 0 {
			continue
		}
		next, r, err := Apply(doc, "", batch)
		if err != nil {
			return nil, &ScriptError{Msg: "applying the script's edits: " + err.Error()}
		}
		doc = next
		res.Notes = append(res.Notes, r.Notes...)
	}
	doc, err = sizeInstances(doc, auto)
	if err != nil {
		return nil, err
	}
	if missing := unplacedMains(doc); len(missing) > 0 {
		return nil, &ScriptError{Msg: "instances of components that were never placed: " + strings.Join(missing, ", ") + ". An instance draws nothing until its main is in the file: add the component (or the variants() result) to library([...]) or place() it"}
	}
	// A new file's empty first page is dropped once the script has made
	// pages of its own, so the file opens on what the script built.
	if len(d.Pages) == 1 && len(d.Pages[0].Children) == 0 && len(doc.Pages) > 1 && len(doc.Pages[0].Children) == 0 && doc.Pages[0].ID == d.Pages[0].ID {
		doc.Pages = doc.Pages[1:]
	}
	for _, p := range doc.Pages {
		for _, n := range p.Children {
			if strings.HasPrefix(n.ID, owned) && !keep[n.ID] {
				sys.Roots[n.ID] = NodeHash(n)
			}
		}
	}
	res.Doc = doc
	res.System = sys
	return res, nil
}

// unplacedMains names the instances whose main component is not in the file.
func unplacedMains(d *Doc) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range d.Pages {
		Walk(p.Children, func(n *Node, _ *Node, _ int) bool {
			if n.Type == TypeInstance && n.ComponentID != "" && !seen[n.ComponentID] && d.Find(n.ComponentID) == nil {
				seen[n.ComponentID] = true
				out = append(out, fmt.Sprintf("%q (%s)", n.Name, n.ComponentID))
			}
			return true
		})
	}
	return out
}

func prevRoot(prev *System, id string) (string, bool) {
	if prev == nil || prev.Roots == nil {
		return "", false
	}
	h, ok := prev.Roots[id]
	return h, ok
}

func evalScript(ctx context.Context, d *Doc, opts ScriptOptions, prefix, src string) (*scriptOut, error) {
	vm := goja.New()
	env, err := json.Marshal(scriptEnv(d, opts.Name, prefix))
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = scriptTimeout
	}
	timer := time.AfterFunc(timeout, func() { vm.Interrupt("the script ran longer than " + timeout.String()) })
	defer timer.Stop()
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("cancelled") })
	defer stop()
	if _, err := vm.RunScript("env", "var __env = "+string(env)+";"); err != nil {
		return nil, err
	}
	// __iconPath gives the path of an icon of the kit's set, for icon swaps.
	if err := vm.Set("__iconPath", func(name string) (string, error) {
		ic, err := LookupIcon(name)
		if err != nil {
			return "", err
		}
		return ic.D, nil
	}); err != nil {
		return nil, err
	}
	// __textWidth measures text in a kit face, so labels the API places
	// (map pins) fit their text.
	if err := vm.Set("__textWidth", func(text, family string, weight int, size float64) float64 {
		return TextWidth(family, weight, size, 0, text)
	}); err != nil {
		return nil, err
	}
	// __qr encodes text as a real, scannable QR code: the module count and a
	// path of one square per dark module, in module units.
	if err := vm.Set("__qr", func(text string) (map[string]any, error) {
		return QRPath(text)
	}); err != nil {
		return nil, err
	}
	if _, err := vm.RunScript("prelude.js", scriptPrelude); err != nil {
		return nil, fmt.Errorf("design script prelude: %w", err)
	}
	name := opts.Name
	if name == "" {
		name = "script"
	}
	// The script runs in its own scope, so it may name a value like a
	// function of the API (const row = ...) without a clash. The wrapper
	// stays on the first line, so line numbers are the script's own.
	if _, err := vm.RunScript(name+".js", "(function () {"+src+"\n})();"); err != nil {
		return nil, &ScriptError{Msg: scriptMessage(err)}
	}
	v, err := vm.RunScript("out", "JSON.stringify({ops: __ops, system: __system})")
	if err != nil {
		return nil, &ScriptError{Msg: scriptMessage(err)}
	}
	var out scriptOut
	if err := json.Unmarshal([]byte(v.String()), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func scriptMessage(err error) string {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		msg := ex.Error()
		// Point at the script, not at the prelude that threw for it.
		if stack := ex.String(); stack != "" {
			for _, line := range strings.Split(stack, "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, ".js:") && !strings.Contains(line, "prelude.js") {
					return msg + " (" + strings.TrimPrefix(line, "at ") + ")"
				}
			}
		}
		return msg
	}
	var intr *goja.InterruptedError
	if errors.As(err, &intr) {
		return fmt.Sprint(intr.Value())
	}
	return err.Error()
}

// markAutoInstances records instances made without a size, which take
// their main's size once the main is laid out.
// autoAxes says which sides of an instance the script left to its content.
type autoAxes struct{ w, h bool }

func markAutoInstances(n map[string]any, into map[string]autoAxes) {
	if t, _ := n["type"].(string); t == string(TypeInstance) {
		_, w := n["width"]
		_, h := n["height"]
		if id, _ := n["id"].(string); id != "" && (!w || !h) {
			into[id] = autoAxes{w: !w, h: !h}
		}
	}
	kids, _ := n["children"].([]any)
	for _, k := range kids {
		if m, ok := k.(map[string]any); ok {
			markAutoInstances(m, into)
		}
	}
}

// sizeInstances gives each auto-sized instance the size its content takes in
// its main's layout (InstanceSize), again until nothing changes: a main that
// holds an instance grows with it.
func sizeInstances(d *Doc, auto map[string]autoAxes) (*Doc, error) {
	if len(auto) == 0 {
		return d, nil
	}
	for range sizePasses {
		var ops []Op
		for id, axes := range auto {
			at := d.Find(id)
			if at == nil || at.Node.Type != TypeInstance {
				continue
			}
			w, h, ok := InstanceSize(d, at.Node)
			if !ok {
				continue
			}
			props := map[string]json.RawMessage{}
			if axes.w && at.Node.SizingX != "fill" && differs(at.Node.Width, w) {
				props["width"], _ = json.Marshal(w)
			}
			if axes.h && at.Node.SizingY != "fill" && differs(at.Node.Height, h) {
				props["height"], _ = json.Marshal(h)
			}
			if len(props) > 0 {
				ops = append(ops, Op{Op: "update", ID: id, Props: props})
			}
		}
		if len(ops) == 0 {
			return d, nil
		}
		sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
		next, _, err := Apply(d, "", ops)
		if err != nil {
			return nil, err
		}
		d = next
	}
	return d, nil
}

// NodeHash is a short hash of a node and everything in it, leaving out what
// layout and text measuring derive (so a re-measure in the app is not taken
// for a hand edit).
func NodeHash(n *Node) string {
	data, _ := json.Marshal(hashForm(n, false))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

func hashForm(n *Node, inLayout bool) map[string]any {
	data, _ := json.Marshal(n)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	delete(m, "children")
	if inLayout && !n.Absolute {
		delete(m, "x")
		delete(m, "y")
	}
	if n.Type == TypeText && n.AutoResize != "none" {
		delete(m, "width")
		delete(m, "height")
	}
	if n.SizingX != "" || (n.Layout != nil && n.Type != TypeText) {
		if n.SizingX == "hug" || n.SizingX == "fill" {
			delete(m, "width")
		}
		if n.SizingY == "hug" || n.SizingY == "fill" {
			delete(m, "height")
		}
	}
	if n.Type == TypeInstance {
		delete(m, "width")
		delete(m, "height")
	}
	kids := make([]any, 0, len(n.Children))
	for _, c := range n.Children {
		kids = append(kids, hashForm(c, n.Layout != nil))
	}
	m["children"] = kids
	return m
}
