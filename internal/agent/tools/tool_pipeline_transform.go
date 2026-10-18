package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// PipelineTransformTool is the pseudo-tool name of a pure data step. It
// reads earlier results of the same pipeline and never calls a tool, reads
// a file, opens a connection or starts a process, so it needs no permission
// and cannot widen what the pipeline is allowed to do.
const PipelineTransformTool = "transform"

const (
	maxTransformItems    = 1000
	maxTransformItemText = 2048
	maxItemsFrom         = 64
	maxTransformPattern  = 256
	// maxPipelineTransformText bounds the text kept per step for transforms.
	maxPipelineTransformText = 1 << 20
)

var transformOps = []string{"lines", "filter", "exclude", "extract", "select", "unique", "sort", "head", "count", "json"}

// pipelineTransform is the argument object of a transform step.
type pipelineTransform struct {
	// Source is an earlier step id (its whole result: JSON when it has
	// one, otherwise its text) or step#/pointer into its JSON result.
	Source  string `json:"source"`
	Op      string `json:"op"`
	Pattern string `json:"pattern,omitempty"`
	Field   string `json:"field,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

func decodeTransform(arguments map[string]any) (pipelineTransform, error) {
	data, err := json.Marshal(arguments)
	if err != nil {
		return pipelineTransform{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var t pipelineTransform
	if err := dec.Decode(&t); err != nil {
		return pipelineTransform{}, fmt.Errorf("transform arguments: %w; fields are source, op, pattern, field and limit", err)
	}
	return t, nil
}

// validateTransformStep checks a transform step before anything runs.
func validateTransformStep(s PipelineStep, seen map[string]bool) string {
	t, err := decodeTransform(s.Arguments)
	if err != nil {
		return err.Error()
	}
	id, _, _ := strings.Cut(t.Source, "#/")
	if !seen[id] {
		return "transform source must be an earlier step id or step#/field, e.g. {\"source\":\"grep1\",\"op\":\"filter\",\"pattern\":\"TODO\"}"
	}
	if !slices.Contains(transformOps, t.Op) {
		return "transform op must be one of " + strings.Join(transformOps, ", ")
	}
	if len(s.Items) > 0 || s.ItemsFrom != "" || len(s.Bindings) > 0 || s.ObservationFrom != "" || s.RequirePassed {
		return "transform takes no items, items_from, bindings, observation_from or require_passed"
	}
	switch t.Op {
	case "filter", "exclude", "extract":
		if t.Pattern == "" || len(t.Pattern) > maxTransformPattern {
			return fmt.Sprintf("transform %s needs a pattern (RE2 syntax, at most %d bytes)", t.Op, maxTransformPattern)
		}
		re, err := regexp.Compile(t.Pattern)
		if err != nil {
			return "transform pattern does not compile: " + err.Error()
		}
		if t.Op == "extract" && re.NumSubexp() != 1 {
			return "transform extract needs exactly one capture group, e.g. \"func (\\\\w+)\""
		}
	case "select":
		if t.Field == "" {
			return "transform select needs field: the key read from each object"
		}
	case "head":
		if t.Limit < 1 {
			return "transform head needs limit >= 1"
		}
	}
	if t.Limit < 0 || t.Limit > maxTransformItems {
		return fmt.Sprintf("transform limit must be 0-%d", maxTransformItems)
	}
	return ""
}

// transformInput resolves the source to a list of elements. A JSON result
// is used as JSON; a text result is split into lines.
func transformInput(t pipelineTransform, texts map[string]string, outputs map[string]json.RawMessage) ([]any, string, error) {
	id, pointer, hasPointer := strings.Cut(t.Source, "#/")
	var value any
	var raw string
	switch {
	case hasPointer:
		v, err := pipelineValue(outputs, id, pointer)
		if err != nil {
			return nil, "", err
		}
		value = v
		data, _ := json.Marshal(v)
		raw = string(data)
	case t.Op == "json" || t.Op == "lines" || t.Op == "extract" || outputs[id] == nil:
		text, ok := texts[id]
		if !ok {
			return nil, "", fmt.Errorf("transform source %s has no successful result", id)
		}
		raw = text
		if t.Op == "json" {
			if err := decodeTransformValue([]byte(text), &value); err != nil {
				return nil, "", fmt.Errorf("transform json: %s is not JSON: %w", id, err)
			}
		} else {
			value = text
		}
	default:
		raw = string(outputs[id])
		if err := decodeTransformValue(outputs[id], &value); err != nil {
			return nil, "", err
		}
	}
	switch v := value.(type) {
	case []any:
		return v, raw, nil
	case string:
		lines := strings.Split(strings.ReplaceAll(v, "\r\n", "\n"), "\n")
		out := make([]any, 0, len(lines))
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				out = append(out, line)
			}
		}
		return out, raw, nil
	default:
		return []any{v}, raw, nil
	}
}

// pipelineValue reads a JSON value, which unlike a binding may be a whole
// array: a transform or items_from consumes every element.
func pipelineValue(outputs map[string]json.RawMessage, id, pointer string) (any, error) {
	var value any
	if err := decodeTransformValue(outputs[id], &value); err != nil {
		return nil, fmt.Errorf("source %s has no JSON result", id)
	}
	if pointer == "" {
		return value, nil
	}
	for _, field := range strings.Split(pointer, "/") {
		field = strings.ReplaceAll(strings.ReplaceAll(field, "~1", "/"), "~0", "~")
		switch container := value.(type) {
		case map[string]any:
			next, ok := container[field]
			if !ok {
				return nil, fmt.Errorf("field %q is missing in %s", field, id)
			}
			value = next
		case []any:
			index, err := strconv.Atoi(field)
			if err != nil || index < 0 || index >= len(container) {
				return nil, fmt.Errorf("index %q is outside the %d results of %s", field, len(container), id)
			}
			value = container[index]
		default:
			return nil, fmt.Errorf("%s#/%s does not address a value", id, pointer)
		}
	}
	return value, nil
}

func decodeTransformValue(data []byte, value *any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("transform source must contain exactly one JSON value")
	}
	return nil
}

// transformNumber compares bounded numbers exactly, without converting
// identifier-sized integers to float64. Exponent bounds limit allocation.
func transformNumber(value any) (*big.Rat, bool, error) {
	var text string
	switch n := value.(type) {
	case json.Number:
		text = string(n)
	case float64:
		text = strconv.FormatFloat(n, 'g', -1, 64)
	default:
		return nil, false, nil
	}
	if len(text) > 256 {
		return nil, true, fmt.Errorf("sort number exceeds 256 digits")
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent < -4096 || exponent > 4096 {
			return nil, true, fmt.Errorf("sort number exponent exceeds comparison bounds")
		}
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, true, fmt.Errorf("invalid sort number")
	}
	return number, true, nil
}

func elementText(v any) string {
	switch e := v.(type) {
	case string:
		return e
	case float64:
		return strconv.FormatFloat(e, 'f', -1, 64)
	case json.Number:
		return string(e)
	case bool:
		return strconv.FormatBool(e)
	default:
		data, _ := json.Marshal(e)
		return string(data)
	}
}

// runTransform computes a transform step. The result keeps the source's
// hash and size so the model can tell exactly what was reduced from what.
func runTransform(arguments map[string]any, texts map[string]string, outputs map[string]json.RawMessage) (json.RawMessage, error) {
	t, err := decodeTransform(arguments)
	if err != nil {
		return nil, err
	}
	in, raw, err := transformInput(t, texts, outputs)
	if err != nil {
		return nil, err
	}
	var re *regexp.Regexp
	if t.Pattern != "" {
		if re, err = regexp.Compile(t.Pattern); err != nil {
			return nil, err
		}
	}
	out := make([]any, 0, len(in))
	switch t.Op {
	case "lines", "json":
		out = in
	case "filter", "exclude":
		for _, e := range in {
			if re.MatchString(elementText(e)) == (t.Op == "filter") {
				out = append(out, e)
			}
		}
	case "extract":
		for _, e := range in {
			for _, m := range re.FindAllStringSubmatch(elementText(e), -1) {
				out = append(out, m[1])
			}
		}
	case "select":
		for _, e := range in {
			if obj, ok := e.(map[string]any); ok {
				if v, ok := obj[t.Field]; ok {
					out = append(out, v)
				}
			}
		}
	case "unique":
		seen := map[string]bool{}
		for _, e := range in {
			encoded, _ := json.Marshal(e)
			key := string(encoded)
			if !seen[key] {
				seen[key] = true
				out = append(out, e)
			}
		}
	case "sort":
		out = append(out, in...)
		for _, value := range out {
			if _, _, err := transformNumber(value); err != nil {
				return nil, err
			}
		}
		slices.SortStableFunc(out, func(a, b any) int {
			x, xn, _ := transformNumber(a)
			y, yn, _ := transformNumber(b)
			if xn && yn {
				return x.Cmp(y)
			}
			if xn {
				return -1
			}
			if yn {
				return 1
			}
			return strings.Compare(elementText(a), elementText(b))
		})
	case "head":
		out = in[:min(len(in), t.Limit)]
	case "count":
		out = in
	}
	limit := maxTransformItems
	if t.Limit > 0 {
		limit = min(limit, t.Limit)
	}
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	for i, e := range out {
		if s, ok := e.(string); ok && len(s) > maxTransformItemText {
			out[i] = strings.ToValidUTF8(s[:maxTransformItemText], "") + " [truncated]"
			truncated = true
		}
	}
	sum := sha256.Sum256([]byte(raw))
	result := map[string]any{
		"op": t.Op, "source": t.Source, "source_sha256": hex.EncodeToString(sum[:8]),
		"input_count": len(in), "count": len(out), "truncated": truncated,
	}
	if t.Op == "count" {
		// The count is of the source elements; listing them would only
		// repeat the source.
		result["count"] = len(in)
	} else {
		result["items"] = out
	}
	return json.Marshal(result)
}

// resolveItemsFrom turns an earlier array result into iteration items.
// Only scalars qualify: an item is substituted for "$item" as text.
func resolveItemsFrom(source string, outputs map[string]json.RawMessage) ([]string, error) {
	id, pointer, _ := strings.Cut(source, "#/")
	var provenance struct {
		Op        string `json:"op"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal(outputs[id], &provenance) == nil && provenance.Op != "" && provenance.Truncated {
		return nil, fmt.Errorf("items_from %s contains truncated transform data; narrow the producing result before using it as tool arguments", source)
	}
	value, err := pipelineValue(outputs, id, pointer)
	if err != nil {
		return nil, fmt.Errorf("items_from %s: %w", source, err)
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("items_from %s is not an array", source)
	}
	if len(list) == 0 {
		return nil, nil
	}
	if len(list) > maxItemsFrom {
		return nil, fmt.Errorf("items_from %s has %d elements; narrow it to at most %d with a transform (filter, unique, head)", source, len(list), maxItemsFrom)
	}
	items := make([]string, 0, len(list))
	for _, e := range list {
		switch e.(type) {
		case string, float64, json.Number, bool:
			text := elementText(e)
			if text == "" || len(text) > 4096 {
				return nil, fmt.Errorf("items_from %s holds an empty or oversized element", source)
			}
			items = append(items, text)
		default:
			return nil, fmt.Errorf("items_from %s holds objects; select a scalar field with a transform first", source)
		}
	}
	return items, nil
}
