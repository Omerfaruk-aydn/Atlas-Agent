package script

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// maxDepth bounds how deep a value may nest when it crosses the boundary, so
// a self-referencing list cannot loop the converter.
const maxDepth = 64

// toGo converts a Starlark value to plain Go data (nil, bool, int64, float64,
// string, []any, map[string]any). Dict keys must be strings, as in JSON.
func toGo(v starlark.Value) (any, error) { return toGoDepth(v, 0) }

func toGoDepth(v starlark.Value, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("value nests more than %d levels", maxDepth)
	}
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.Int:
		if i, ok := x.Int64(); ok {
			return i, nil
		}
		f, _ := starlark.AsFloat(x)
		return f, nil
	case starlark.Float:
		return float64(x), nil
	case starlark.String:
		return string(x), nil
	case *starlark.List:
		return sequence(x, depth)
	case starlark.Tuple:
		return sequence(x, depth)
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, item := range x.Items() {
			key, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("dict key %s is a %s; keys must be strings", item[0], item[0].Type())
			}
			val, err := toGoDepth(item[1], depth+1)
			if err != nil {
				return nil, err
			}
			out[string(key)] = val
		}
		return out, nil
	case *starlarkstruct.Struct:
		out := map[string]any{}
		for _, name := range x.AttrNames() {
			attr, err := x.Attr(name)
			if err != nil {
				return nil, err
			}
			val, err := toGoDepth(attr, depth+1)
			if err != nil {
				return nil, err
			}
			out[name] = val
		}
		return out, nil
	}
	return nil, fmt.Errorf("a %s cannot be converted to JSON", v.Type())
}

func sequence(s starlark.Indexable, depth int) (any, error) {
	out := make([]any, s.Len())
	for i := range out {
		val, err := toGoDepth(s.Index(i), depth+1)
		if err != nil {
			return nil, err
		}
		out[i] = val
	}
	return out, nil
}

// toStarlark converts decoded JSON data to a Starlark value. A number that is
// a whole number becomes an int, so a count read from JSON can index a list.
func toStarlark(v any) (starlark.Value, error) { return toStarlarkDepth(v, 0) }

func toStarlarkDepth(v any, depth int) (starlark.Value, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("value nests more than %d levels", maxDepth)
	}
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(x), nil
	case string:
		return starlark.String(x), nil
	case int:
		return starlark.MakeInt(x), nil
	case int64:
		return starlark.MakeInt64(x), nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return starlark.MakeInt64(int64(x)), nil
		}
		return starlark.Float(x), nil
	case []any:
		items := make([]starlark.Value, len(x))
		for i, item := range x {
			val, err := toStarlarkDepth(item, depth+1)
			if err != nil {
				return nil, err
			}
			items[i] = val
		}
		return starlark.NewList(items), nil
	case map[string]any:
		d := starlark.NewDict(len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			val, err := toStarlarkDepth(x[k], depth+1)
			if err != nil {
				return nil, err
			}
			if err := d.SetKey(starlark.String(k), val); err != nil {
				return nil, err
			}
		}
		return d, nil
	}
	return nil, fmt.Errorf("a %T cannot be converted to a script value", v)
}

func marshalIndent(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	return string(data), err
}
