package qualityflows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type Assumption struct {
	Name             string          `json:"name"`
	File             string          `json:"file"`
	Pointer          string          `json:"pointer"`
	Rule             string          `json:"rule"`
	Field            string          `json:"field,omitempty"`
	Expected         json.RawMessage `json:"expected,omitempty"`
	Min              *int64          `json:"min,omitempty"`
	Max              *int64          `json:"max,omitempty"`
	ReferenceFile    string          `json:"reference_file,omitempty"`
	ReferencePointer string          `json:"reference_pointer,omitempty"`
	ReferenceField   string          `json:"reference_field,omitempty"`
}

type AssumptionSpec struct {
	Version int          `json:"version"`
	Checks  []Assumption `json:"checks"`
}

type AssumptionResult struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Pointer  string `json:"pointer"`
	Rule     string `json:"rule"`
	Passed   bool   `json:"passed"`
	Observed bool   `json:"observed"`
	Reason   string `json:"reason,omitempty"`
	// Counterexample identifies the offending row without echoing its data.
	Counterexample *int `json:"counterexample_index,omitempty"`
}

type AssumptionReport struct {
	Passed      bool               `json:"passed"`
	SpecHash    string             `json:"spec_hash"`
	InputHashes map[string]string  `json:"input_hashes"`
	Results     []AssumptionResult `json:"results"`
}

func jsonValue(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON document")
	}
	nodes := 0
	var validate func(any, int) error
	validate = func(value any, depth int) error {
		nodes++
		if nodes > 100000 || depth > 64 {
			return fmt.Errorf("JSON exceeds structural limits")
		}
		switch v := value.(type) {
		case json.Number:
			if _, ok := decimalRat(v); !ok {
				return fmt.Errorf("numeric precision/exponent exceeds limits")
			}
		case []any:
			for _, item := range v {
				if err := validate(item, depth+1); err != nil {
					return err
				}
			}
		case map[string]any:
			for _, item := range v {
				if err := validate(item, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := validate(value, 0); err != nil {
		return nil, err
	}
	return value, nil
}

func decimalRat(number json.Number) (*big.Rat, bool) {
	s := string(number)
	if len(s) > 1024 {
		return nil, false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(s[i+1:])
		if err != nil || exponent < -1024 || exponent > 1024 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(s)
}

func validPointer(pointer string) bool {
	if len(pointer) > 4096 || (pointer != "" && !strings.HasPrefix(pointer, "/")) {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' {
			i++
			if i == len(pointer) || (pointer[i] != '0' && pointer[i] != '1') {
				return false
			}
		}
	}
	return true
}

// ResolvePointer implements RFC 6901, including escaped object keys.
func ResolvePointer(value any, pointer string) (any, error) {
	if !validPointer(pointer) {
		return nil, fmt.Errorf("invalid JSON pointer")
	}
	if pointer == "" {
		return value, nil
	}
	for _, part := range strings.Split(pointer[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				return nil, fmt.Errorf("object key not found")
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) || strconv.Itoa(i) != part {
				return nil, fmt.Errorf("array index not found")
			}
			value = v[i]
		default:
			return nil, fmt.Errorf("pointer traverses a scalar")
		}
	}
	return value, nil
}

func ValidateAssumptions(spec AssumptionSpec) error {
	if spec.Version != 1 || len(spec.Checks) < 1 || len(spec.Checks) > 64 {
		return fmt.Errorf("version 1 and 1-64 checks required")
	}
	names := map[string]bool{}
	for _, check := range spec.Checks {
		if check.Name == "" || len(check.Name) > 128 || names[check.Name] || !validPointer(check.Pointer) || !validPointer(check.Field) || !validPointer(check.ReferencePointer) || !validPointer(check.ReferenceField) {
			return fmt.Errorf("invalid assumption identity or pointer")
		}
		names[check.Name] = true
		if _, err := ProjectPath(".", check.File); err != nil {
			return err
		}
		if len(check.Expected) > 16384 {
			return fmt.Errorf("expected value too large")
		}
		switch check.Rule {
		case "exists", "nonempty", "unique", "required":
		case "equals", "sum_equals":
			value, err := jsonValue(check.Expected)
			if err != nil {
				return fmt.Errorf("rule requires expected JSON value")
			}
			if check.Rule == "sum_equals" {
				if _, ok := value.(json.Number); !ok {
					return fmt.Errorf("sum_equals needs numeric expected")
				}
			}
		case "count", "range":
			if check.Min == nil && check.Max == nil || check.Min != nil && check.Max != nil && *check.Min > *check.Max {
				return fmt.Errorf("rule requires ordered min/max bounds")
			}
			if check.Rule == "count" && (check.Min != nil && *check.Min < 0 || check.Max != nil && *check.Max < 0) {
				return fmt.Errorf("count cannot be negative")
			}
		case "references":
			if _, err := ProjectPath(".", check.ReferenceFile); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported assumption rule: %s", check.Rule)
		}
	}
	return nil
}

// canonicalJSON treats equivalent numeric spellings as equal without rounding.
func canonicalJSON(value any) string {
	switch v := value.(type) {
	case json.Number:
		if n, ok := decimalRat(v); ok {
			return "number:" + n.RatString()
		}
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = canonicalJSON(item)
		}
		encoded, _ := json.Marshal(parts)
		return "array:" + string(encoded)
	case map[string]any:
		fields := map[string]string{}
		for key, item := range v {
			fields[key] = canonicalJSON(item)
		}
		encoded, _ := json.Marshal(fields)
		return "object:" + string(encoded)
	}
	encoded, _ := json.Marshal(value)
	return fmt.Sprintf("%T:%s", value, encoded)
}

func evaluateAssumption(check Assumption, value, reference any) (string, *int) {
	bad := func(reason string, index int) (string, *int) { return reason, &index }
	if check.Rule == "exists" {
		return "", nil
	}
	if check.Rule == "equals" {
		expected, _ := jsonValue(check.Expected)
		if canonicalJSON(value) != canonicalJSON(expected) {
			return "value differs from expected", nil
		}
		return "", nil
	}
	if check.Rule == "nonempty" {
		switch v := value.(type) {
		case string:
			if len(v) > 0 {
				return "", nil
			}
		case []any:
			if len(v) > 0 {
				return "", nil
			}
		case map[string]any:
			if len(v) > 0 {
				return "", nil
			}
		}
		return "value is empty, null or not a collection/string", nil
	}
	rows, ok := value.([]any)
	if !ok {
		return "rule requires an array", nil
	}
	if check.Rule == "count" {
		if check.Min != nil && int64(len(rows)) < *check.Min || check.Max != nil && int64(len(rows)) > *check.Max {
			return "array count outside bounds", nil
		}
		return "", nil
	}
	seen := map[string]bool{}
	if check.Rule == "references" {
		parents, ok := reference.([]any)
		if !ok {
			return "reference requires an array", nil
		}
		for _, parent := range parents {
			v, err := ResolvePointer(parent, check.ReferenceField)
			if err != nil || v == nil {
				return "reference key missing/null", nil
			}
			seen[canonicalJSON(v)] = true
		}
	}
	sum := new(big.Rat)
	for i, row := range rows {
		v, err := ResolvePointer(row, check.Field)
		if err != nil {
			return bad("row field missing", i)
		}
		switch check.Rule {
		case "required":
			if v == nil || v == "" {
				return bad("required field is null/empty", i)
			}
		case "unique":
			key := canonicalJSON(v)
			if v == nil || seen[key] {
				return bad("duplicate or null key", i)
			}
			seen[key] = true
		case "references":
			if v == nil || !seen[canonicalJSON(v)] {
				return bad("reference has no matching parent", i)
			}
		case "range", "sum_equals":
			number, ok := v.(json.Number)
			if !ok {
				return bad("field is not numeric", i)
			}
			n, ok := decimalRat(number)
			if !ok {
				return bad("invalid number", i)
			}
			if check.Rule == "sum_equals" {
				sum.Add(sum, n)
			} else if check.Min != nil && n.Cmp(new(big.Rat).SetInt64(*check.Min)) < 0 || check.Max != nil && n.Cmp(new(big.Rat).SetInt64(*check.Max)) > 0 {
				return bad("number outside bounds", i)
			}
		}
	}
	if check.Rule == "sum_equals" {
		expected, _ := jsonValue(check.Expected)
		n, _ := decimalRat(expected.(json.Number))
		if sum.Cmp(n) != 0 {
			return "sum differs from expected", nil
		}
	}
	return "", nil
}

func RunAssumptions(ctx context.Context, root string, spec AssumptionSpec) (AssumptionReport, error) {
	report := AssumptionReport{InputHashes: map[string]string{}, Results: []AssumptionResult{}}
	if err := ValidateAssumptions(spec); err != nil {
		return report, err
	}
	encoded, _ := json.Marshal(spec)
	report.SpecHash = engineering.Hash(string(encoded))
	cache := map[string]any{}
	load := func(file string) (any, error) {
		if value, ok := cache[file]; ok {
			return value, nil
		}
		if len(cache) >= 32 {
			return nil, fmt.Errorf("too many input documents")
		}
		data, err := ReadFile(root, file, 1024*1024)
		if err != nil {
			return nil, err
		}
		value, err := jsonValue(data)
		if err != nil {
			return nil, err
		}
		cache[file] = value
		report.InputHashes[file] = engineering.Hash(string(data))
		return value, nil
	}
	report.Passed = true
	for _, check := range spec.Checks {
		if err := ctx.Err(); err != nil {
			report.Passed = false
			return report, err
		}
		result := AssumptionResult{Name: check.Name, File: check.File, Pointer: check.Pointer, Rule: check.Rule}
		value, err := load(check.File)
		var reference any
		if err == nil {
			result.Observed = true
			value, err = ResolvePointer(value, check.Pointer)
		}
		if err == nil && check.Rule == "references" {
			reference, err = load(check.ReferenceFile)
			if err == nil {
				reference, err = ResolvePointer(reference, check.ReferencePointer)
			}
		}
		if err != nil {
			result.Reason = err.Error()
		} else {
			result.Reason, result.Counterexample = evaluateAssumption(check, value, reference)
		}
		result.Passed = result.Reason == ""
		report.Passed = report.Passed && result.Passed
		report.Results = append(report.Results, result)
	}
	return report, nil
}
