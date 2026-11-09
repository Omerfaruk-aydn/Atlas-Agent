package design

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tokenOp runs the ops that edit shared styles and variables. It reports
// false for any other op.
func (d *Doc) tokenOp(i int, op Op, seen map[string]bool, res *Result) (bool, error) {
	switch op.Op {
	case "set_style":
		return true, d.setStyle(i, op, seen, res)
	case "delete_style":
		return true, d.deleteStyle(op)
	case "set_variable":
		return true, d.setVariable(i, op, seen, res)
	case "delete_variable":
		if d.Variables == nil || d.Variables.Items[op.ID] == nil {
			return true, fmt.Errorf("no variable %q", op.ID)
		}
		delete(d.Variables.Items, op.ID)
		return true, nil
	case "set_modes":
		return true, d.setModes(op)
	}
	return false, nil
}

func (d *Doc) freshID(seen map[string]bool) string {
	id := NewID()
	for seen[id] {
		id = NewID()
	}
	seen[id] = true
	return id
}

// styleJSON is the op's props with its name, as the style's JSON.
func styleJSON(op Op) ([]byte, error) {
	fields := map[string]json.RawMessage{}
	for k, v := range op.Props {
		fields[k] = v
	}
	if op.Name != "" {
		name, _ := json.Marshal(op.Name)
		fields["name"] = name
	}
	return json.Marshal(fields)
}

func (d *Doc) setStyle(i int, op Op, seen map[string]bool, res *Result) error {
	if d.Styles == nil {
		d.Styles = &Styles{}
	}
	id := op.ID
	if id == "" {
		id = d.freshID(seen)
		res.Created[i] = id
	} else if !validRef(id) {
		return fmt.Errorf("style id %q is not valid", id)
	}
	data, err := styleJSON(op)
	if err != nil {
		return err
	}
	switch op.Kind {
	case "fill":
		s := d.Styles.Fills[id]
		if s == nil {
			s = &FillStyle{}
		}
		if err := json.Unmarshal(data, s); err != nil {
			return fmt.Errorf("fill style props: %w", err)
		}
		if len(sanitizePaints(s.Fills)) == 0 {
			return fmt.Errorf("a fill style needs fills")
		}
		if d.Styles.Fills == nil {
			d.Styles.Fills = map[string]*FillStyle{}
		}
		d.Styles.Fills[id] = s
	case "effect":
		s := d.Styles.Effects[id]
		if s == nil {
			s = &EffectStyle{}
		}
		if err := json.Unmarshal(data, s); err != nil {
			return fmt.Errorf("effect style props: %w", err)
		}
		if len(sanitizeEffects(s.Effects)) == 0 {
			return fmt.Errorf("an effect style needs effects")
		}
		if d.Styles.Effects == nil {
			d.Styles.Effects = map[string]*EffectStyle{}
		}
		d.Styles.Effects[id] = s
	case "text":
		s := d.Styles.Text[id]
		if s == nil {
			s = &TextStyle{Font: Font{Family: "Inter", Size: 16, Weight: 400}}
		}
		// A font in the props merges into the style's font, like a node's.
		if raw, ok := op.Props["font"]; ok {
			f := s.Font
			if err := json.Unmarshal(raw, &f); err != nil {
				return fmt.Errorf("font must be an object: %w", err)
			}
			s.Font = f
			delete(op.Props, "font")
			if data, err = styleJSON(op); err != nil {
				return err
			}
		}
		keep := s.Font
		if err := json.Unmarshal(data, s); err != nil {
			return fmt.Errorf("text style props: %w", err)
		}
		if s.Font == (Font{}) {
			s.Font = keep
		}
		if d.Styles.Text == nil {
			d.Styles.Text = map[string]*TextStyle{}
		}
		d.Styles.Text[id] = s
	default:
		return fmt.Errorf("style kind must be fill, text or effect, not %q", op.Kind)
	}
	return nil
}

func (d *Doc) deleteStyle(op Op) error {
	if d.Styles == nil {
		return fmt.Errorf("no style %q", op.ID)
	}
	var found bool
	switch op.Kind {
	case "fill":
		_, found = d.Styles.Fills[op.ID]
		delete(d.Styles.Fills, op.ID)
	case "effect":
		_, found = d.Styles.Effects[op.ID]
		delete(d.Styles.Effects, op.ID)
	case "text":
		_, found = d.Styles.Text[op.ID]
		delete(d.Styles.Text, op.ID)
	default:
		return fmt.Errorf("style kind must be fill, text or effect, not %q", op.Kind)
	}
	if !found {
		return fmt.Errorf("no %s style %q", op.Kind, op.ID)
	}
	// Nodes that pointed at it go back to their own fills, text and effects.
	return nil
}

func (d *Doc) setVariable(i int, op Op, seen map[string]bool, res *Result) error {
	if d.Variables == nil {
		d.Variables = &Variables{Modes: []string{"Default"}, Mode: "Default", Items: map[string]*Variable{}}
	}
	id := op.ID
	if id == "" {
		id = d.freshID(seen)
		res.Created[i] = id
	} else if !validRef(id) {
		return fmt.Errorf("variable id %q is not valid", id)
	}
	v := d.Variables.Items[id]
	if v == nil {
		v = &Variable{Values: map[string]string{}}
	}
	if op.Name != "" {
		v.Name = op.Name
	}
	if raw, ok := op.Props["values"]; ok {
		var values map[string]string
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("values must map mode names to #RRGGBB colours: %w", err)
		}
		for m, c := range values {
			if !IsColor(c) {
				return fmt.Errorf("value %q for mode %q is not a #RRGGBB colour", c, m)
			}
			found := false
			for _, known := range d.Variables.Modes {
				found = found || known == m
			}
			if !found {
				return fmt.Errorf("unknown mode %q (modes: %s); add it with set_modes", m, strings.Join(d.Variables.Modes, ", "))
			}
			v.Values[m] = strings.ToUpper(c)
		}
	}
	if d.Variables.Items == nil {
		d.Variables.Items = map[string]*Variable{}
	}
	d.Variables.Items[id] = v
	return nil
}

func (d *Doc) setModes(op Op) error {
	if d.Variables == nil {
		d.Variables = &Variables{Items: map[string]*Variable{}}
	}
	if len(op.Modes) > 0 {
		d.Variables.Modes = op.Modes
	}
	if op.Mode != "" {
		found := false
		for _, m := range d.Variables.Modes {
			found = found || m == op.Mode
		}
		if !found {
			return fmt.Errorf("mode %q is not one of the modes %s", op.Mode, strings.Join(d.Variables.Modes, ", "))
		}
		d.Variables.Mode = op.Mode
	}
	return nil
}
