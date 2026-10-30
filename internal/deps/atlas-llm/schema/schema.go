// Package schema provides JSON schema generation and validation utilities.
// It supports automatic schema generation from Go types and validation of parsed objects.
package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/jsonrepair"
	"github.com/kaptinlin/jsonschema"
)

// ObjectRepairFunc is a function that attempts to repair invalid JSON output.
// It receives the raw text and the error encountered during parsing or validation,
// and returns repaired text or an error if repair is not possible.
type ObjectRepairFunc func(ctx context.Context, text string, err error) (string, error)

// ParseError is returned when object generation fails
// due to parsing errors, validation errors, or model failures.
type ParseError struct {
	RawText         string
	ParseError      error
	ValidationError error
}

// Schema represents a JSON schema for tool input validation.
type Schema struct {
	Type        string             `json:"type,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Description string             `json:"description,omitempty"`
	Enum        []any              `json:"enum,omitempty"`
	Format      string             `json:"format,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	// Any marks a value whose Go type cannot describe its wire format: an
	// interface, a json.RawMessage, or a type with its own UnmarshalJSON. The
	// advertised type is a hint for the model; validation accepts any value.
	Any bool `json:"-"`
}

// ParseState represents the state of JSON parsing.
type ParseState string

const (
	// ParseStateUndefined means input was undefined/empty.
	ParseStateUndefined ParseState = "undefined"

	// ParseStateSuccessful means JSON parsed without repair.
	ParseStateSuccessful ParseState = "successful"

	// ParseStateRepaired means JSON parsed after repair.
	ParseStateRepaired ParseState = "repaired"

	// ParseStateFailed means JSON could not be parsed even after repair.
	ParseStateFailed ParseState = "failed"
)

// ToParameters converts a Schema to the parameters map format expected by ToolInfo.
func ToParameters(s Schema) map[string]any {
	if s.Properties == nil {
		return make(map[string]any)
	}

	result := make(map[string]any)
	for name, propSchema := range s.Properties {
		result[name] = ToMap(*propSchema)
	}
	return result
}

// Provider lets a type describe its wire contract when reflection would
// describe its Go representation instead, for example a raw JSON holder
// whose bytes must arrive as a JSON object rather than an integer array.
// The method must have a value receiver and must not depend on the value.
type Provider interface {
	JSONSchema() Schema
}

var providerType = reflect.TypeFor[Provider]()

// Generate generates a JSON schema from a reflect.Type.
// It recursively processes struct fields, arrays, maps, and primitive types.
var timeType = reflect.TypeOf(time.Time{})

func Generate(t reflect.Type) Schema {
	return generateSchemaRecursive(t, make(map[reflect.Type]bool))
}

var (
	rawMessageType  = reflect.TypeFor[json.RawMessage]()
	unmarshalerType = reflect.TypeFor[json.Unmarshaler]()
)

func generateSchemaRecursive(t reflect.Type, visited map[reflect.Type]bool) Schema {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Implements(providerType) {
		return reflect.Zero(t).Interface().(Provider).JSONSchema()
	}
	// A raw JSON holder is a byte slice to reflection, which would advertise
	// an array of integers. Its wire format is any JSON value.
	if t == rawMessageType {
		return Schema{Any: true}
	}
	generated := generateByKind(t, visited)
	// A type that decodes itself has a wire format reflection cannot see.
	if t != timeType && t.Kind() != reflect.Interface && reflect.PointerTo(t).Implements(unmarshalerType) {
		generated.Any = true
	}
	return generated
}

func generateByKind(t reflect.Type, visited map[reflect.Type]bool) Schema {
	// time.Time marshals to an RFC 3339 string, not to its unexported
	// struct fields; describing it as an object made every encoded value
	// fail validation and told the model the wrong shape.
	if t == timeType {
		return Schema{Type: "string", Format: "date-time"}
	}

	if visited[t] {
		return Schema{Type: "object"}
	}
	visited[t] = true
	defer delete(visited, t)

	switch t.Kind() {
	case reflect.String:
		return Schema{Type: "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return Schema{Type: "number"}
	case reflect.Bool:
		return Schema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		itemSchema := generateSchemaRecursive(t.Elem(), visited)
		return Schema{
			Type:  "array",
			Items: &itemSchema,
		}
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			valueSchema := generateSchemaRecursive(t.Elem(), visited)
			schema := Schema{
				Type: "object",
				Properties: map[string]*Schema{
					"*": &valueSchema,
				},
			}
			return schema
		}
		return Schema{Type: "object"}
	case reflect.Struct:
		schema := Schema{
			Type:       "object",
			Properties: make(map[string]*Schema),
		}
		for field := range t.Fields() {
			field := field

			if !field.IsExported() {
				continue
			}

			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}

			fieldName := field.Name
			required := true

			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}

				if slices.Contains(parts[1:], "omitempty") {
					required = false
				}
			} else {
				fieldName = toSnakeCase(fieldName)
			}

			fieldSchema := generateSchemaRecursive(field.Type, visited)

			if desc := field.Tag.Get("description"); desc != "" {
				fieldSchema.Description = desc
			}

			if enumTag := field.Tag.Get("enum"); enumTag != "" {
				enumValues := strings.Split(enumTag, ",")
				fieldSchema.Enum = make([]any, len(enumValues))
				for i, v := range enumValues {
					fieldSchema.Enum[i] = strings.TrimSpace(v)
				}
			}

			schema.Properties[fieldName] = &fieldSchema

			if required {
				schema.Required = append(schema.Required, fieldName)
			}
		}

		return schema
	case reflect.Interface:
		// Any JSON value is acceptable; "object" is only the hint advertised.
		return Schema{Type: "object", Any: true}
	default:
		return Schema{Type: "object"}
	}
}

// ToMap converts a Schema to a map representation suitable for JSON Schema.
func ToMap(schema Schema) map[string]any {
	result := make(map[string]any)

	if schema.Type != "" {
		result["type"] = schema.Type
	}

	if schema.Description != "" {
		result["description"] = schema.Description
	}

	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}

	if schema.Format != "" {
		result["format"] = schema.Format
	}

	if schema.Minimum != nil {
		result["minimum"] = *schema.Minimum
	}

	if schema.Maximum != nil {
		result["maximum"] = *schema.Maximum
	}

	if schema.MinLength != nil {
		result["minLength"] = *schema.MinLength
	}

	if schema.MaxLength != nil {
		result["maxLength"] = *schema.MaxLength
	}

	if schema.Properties != nil {
		props := make(map[string]any)
		for name, propSchema := range schema.Properties {
			props[name] = ToMap(*propSchema)
		}
		result["properties"] = props
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	if schema.Items != nil {
		itemsMap := ToMap(*schema.Items)
		// Ensure type is always set for items, even if it was blank for llama.cpp compatibility
		if _, hasType := itemsMap["type"]; !hasType && schema.Items.Type == "" {
			if len(schema.Items.Properties) > 0 {
				itemsMap["type"] = "object"
			}
		}
		result["items"] = itemsMap
	}

	return result
}

// ParsePartialJSON attempts to parse potentially incomplete JSON.
// It first tries standard JSON parsing, then attempts repair if that fails.
//
// Returns:
//   - result: The parsed JSON value (map, slice, or primitive)
//   - state: Indicates whether parsing succeeded, needed repair, or failed
//   - err: The error if parsing failed completely
//
// Example:
//
//	obj, state, err := ParsePartialJSON(`{"name": "John", "age": 25`)
//	// Result: map[string]any{"name": "John", "age": 25}, ParseStateRepaired, nil
func ParsePartialJSON(text string) (any, ParseState, error) {
	if text == "" {
		return nil, ParseStateUndefined, nil
	}

	var result any
	if err := json.Unmarshal([]byte(text), &result); err == nil {
		return result, ParseStateSuccessful, nil
	}

	repaired, err := jsonrepair.RepairJSON(text)
	if err != nil {
		return nil, ParseStateFailed, fmt.Errorf("json repair failed: %w", err)
	}

	if err := json.Unmarshal([]byte(repaired), &result); err != nil {
		return nil, ParseStateFailed, fmt.Errorf("failed to parse repaired json: %w", err)
	}

	return result, ParseStateRepaired, nil
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.ValidationError != nil {
		return fmt.Sprintf("object validation failed: %v", e.ValidationError)
	}
	if e.ParseError != nil {
		return fmt.Sprintf("failed to parse object: %v", e.ParseError)
	}
	return "failed to generate object"
}

// ParseAndValidate combines JSON parsing and validation.
// Returns the parsed object if both parsing and validation succeed.
func ParseAndValidate(text string, schema Schema) (any, error) {
	obj, state, err := ParsePartialJSON(text)
	if state == ParseStateFailed {
		return nil, &ParseError{
			RawText:    text,
			ParseError: err,
		}
	}

	if err := validateAgainstSchema(obj, schema); err != nil {
		return nil, &ParseError{
			RawText:         text,
			ValidationError: err,
		}
	}

	return obj, nil
}

// ValidateAgainstSchema validates a parsed object against a Schema.
func ValidateAgainstSchema(obj any, schema Schema) error {
	return validateAgainstSchema(obj, schema)
}

func validateAgainstSchema(obj any, schema Schema) error {
	jsonSchemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	validator, err := compiler.Compile(jsonSchemaBytes)
	if err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	result := validator.Validate(obj)
	if !result.IsValid() {
		return fmt.Errorf("validation failed: %s", strings.Join(ValidationMessages(result), "; "))
	}

	return nil
}

// wrapperKeywords only say that a nested schema failed; the nested failure
// carries the useful field and reason.
var wrapperKeywords = map[string]bool{
	"properties": true, "items": true, "prefixItems": true, "allOf": true,
	"$ref": true, "additionalProperties": true, "patternProperties": true,
}

// ValidationMessages names each failing field by its JSON pointer and says
// why it failed. Messages have their parameters filled in, and keywords
// that only report a nested failure are left out when that nested failure
// is listed, so no unresolved placeholder or bare "does not match the
// schema" reaches the model.
func ValidationMessages(result *jsonschema.EvaluationResult) []string {
	list := result.ToList(false)
	entries := append([]jsonschema.List{*list}, list.Details...)
	var leaves, wrappers []string
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.Valid && len(entry.Errors) == 0 {
			continue
		}
		where := entry.InstanceLocation
		if where == "" {
			where = "/"
		}
		keys := make([]string, 0, len(entry.Errors))
		for keyword := range entry.Errors {
			keys = append(keys, keyword)
		}
		slices.Sort(keys)
		for _, keyword := range keys {
			message := entry.Errors[keyword]
			if strings.Contains(message, "{") && strings.Contains(message, "}") {
				message = keyword + " constraint failed"
			}
			line := fmt.Sprintf("%s: %s", where, message)
			if seen[line] {
				continue
			}
			seen[line] = true
			if wrapperKeywords[keyword] {
				wrappers = append(wrappers, line)
			} else {
				leaves = append(leaves, line)
			}
		}
	}
	if len(leaves) > 0 {
		return leaves
	}
	if len(wrappers) > 0 {
		return wrappers
	}
	return []string{"/: value does not match the schema"}
}

// ParseAndValidateWithRepair attempts parsing, validation, and custom repair.
func ParseAndValidateWithRepair(
	ctx context.Context,
	text string,
	schema Schema,
	repair ObjectRepairFunc,
) (any, error) {
	obj, state, parseErr := ParsePartialJSON(text)

	if state == ParseStateSuccessful || state == ParseStateRepaired {
		validationErr := validateAgainstSchema(obj, schema)
		if validationErr == nil {
			return obj, nil
		}

		if repair != nil {
			repairedText, repairErr := repair(ctx, text, validationErr)
			if repairErr == nil {
				obj2, state2, _ := ParsePartialJSON(repairedText)
				if state2 == ParseStateSuccessful || state2 == ParseStateRepaired {
					if err := validateAgainstSchema(obj2, schema); err == nil {
						return obj2, nil
					}
				}
			}
		}

		return nil, &ParseError{
			RawText:         text,
			ValidationError: validationErr,
		}
	}

	if repair != nil {
		repairedText, repairErr := repair(ctx, text, parseErr)
		if repairErr == nil {
			obj2, state2, parseErr2 := ParsePartialJSON(repairedText)
			if state2 == ParseStateSuccessful || state2 == ParseStateRepaired {
				if err := validateAgainstSchema(obj2, schema); err == nil {
					return obj2, nil
				}
			}
			return nil, &ParseError{
				RawText:    repairedText,
				ParseError: parseErr2,
			}
		}
	}

	return nil, &ParseError{
		RawText:    text,
		ParseError: parseErr,
	}
}

func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// Normalize recursively normalizes a raw JSON Schema map so it is
// compatible with providers that reject type-arrays (e.g. OpenAI). Type
// arrays are converted to anyOf and bare "array" types get "items":{}.
func Normalize(node map[string]any) {
	for _, child := range node {
		switch v := child.(type) {
		case map[string]any:
			Normalize(v)
		case []any:
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					Normalize(m)
				}
			}
		}
	}

	typeArr, ok := node["type"].([]any)
	if !ok {
		if node["type"] == "array" {
			if _, has := node["items"]; !has {
				node["items"] = map[string]any{}
			}
		}
		return
	}

	anyOf := make([]any, 0, len(typeArr))
	for _, t := range typeArr {
		variant := map[string]any{"type": t}
		if t == "array" {
			variant["items"] = map[string]any{}
		}
		anyOf = append(anyOf, variant)
	}
	delete(node, "type")
	node["anyOf"] = anyOf
}

// ValidationMap is the exact contract of a generated schema, for checking
// input rather than advertising it. It differs from ToMap where generation
// can only hint: an opaque value (see Schema.Any) accepts anything, and a map
// type constrains its values instead of naming a property called "*".
func ValidationMap(s Schema) map[string]any {
	if s.Any {
		result := map[string]any{}
		if s.Description != "" {
			result["description"] = s.Description
		}
		if len(s.Enum) > 0 {
			result["enum"] = s.Enum
		}
		return result
	}
	result := ToMapShallow(s)
	if value, ok := s.Properties["*"]; ok && s.Type == "object" && len(s.Properties) == 1 {
		result["additionalProperties"] = ValidationMap(*value)
		return result
	}
	if s.Properties != nil {
		props := make(map[string]any, len(s.Properties))
		for name, property := range s.Properties {
			props[name] = ValidationMap(*property)
		}
		result["properties"] = props
	}
	if s.Items != nil {
		result["items"] = ValidationMap(*s.Items)
	}
	return result
}

// ValidationParameters is ValidationMap for a tool's top-level properties,
// in the same shape as ToParameters.
func ValidationParameters(s Schema) map[string]any {
	if s.Properties == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(s.Properties))
	for name, property := range s.Properties {
		result[name] = ValidationMap(*property)
	}
	return result
}

// ToMapShallow converts one schema node without its children.
func ToMapShallow(s Schema) map[string]any {
	s.Properties, s.Items = nil, nil
	return ToMap(s)
}

// InputPolicy says what a tool's contract does with fields it does not list.
type InputPolicy int

const (
	// IgnoreUnknown lets a call carry fields the tool does not define; the
	// tool never sees them. It is the default, because models routinely add
	// harmless extras and refusing them costs a round trip.
	IgnoreUnknown InputPolicy = iota
	// RejectUnknown refuses a call with an undefined top-level field, for tools
	// where a misspelled option would otherwise be dropped silently and the
	// action run with different behavior than the model asked for.
	RejectUnknown
)

// ValidateExact checks input against an exact contract (see ValidationMap).
func ValidateExact(parameters map[string]any, required []string, policy InputPolicy, input map[string]any) error {
	if len(parameters) == 0 {
		return nil
	}
	doc := map[string]any{"type": "object", "properties": parameters}
	if len(required) > 0 {
		doc["required"] = required
	}
	if policy == RejectUnknown {
		doc["additionalProperties"] = false
	}
	return validateDocument(doc, input)
}

func validateDocument(doc map[string]any, input map[string]any) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return nil //nolint:nilerr // An unrepresentable schema cannot reject input.
	}
	validator, err := jsonschema.NewCompiler().Compile(data)
	if err != nil {
		return nil //nolint:nilerr // An invalid schema is the tool author's bug, not the model's.
	}
	if result := validator.Validate(input); !result.IsValid() {
		return fmt.Errorf("invalid arguments: %s", strings.Join(ValidationMessages(result), "; "))
	}
	return nil
}

// ValidateToolInput checks a tool call's arguments against the schema a tool
// advertises (its property map and required list): required fields, types,
// enums and nested structure. It is the fallback for tools whose schema was
// written by hand, where which parts are exact is unknown, so it prunes the
// placeholder types generation uses (see reliableProperties). Tools built from
// a Go type carry an exact contract and use ValidateExact.
func ValidateToolInput(parameters map[string]any, required []string, input map[string]any) error {
	if len(parameters) == 0 {
		return nil
	}
	doc := map[string]any{"type": "object", "properties": reliableProperties(parameters)}
	if len(required) > 0 {
		doc["required"] = required
	}
	return validateDocument(doc, input)
}

// reliableProperties keeps the parts of a generated schema that describe the
// wire format exactly: primitive types, enums and required fields. Generation
// advertises an interface or json.RawMessage field as an "object" or an array
// of integers, which is a placeholder for "anything", so those type claims
// are dropped rather than enforced.
func reliableProperties(properties map[string]any) map[string]any {
	out := make(map[string]any, len(properties))
	for name, property := range properties {
		out[name] = reliableNode(property)
	}
	return out
}

func reliableNode(node any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return node
	}
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	switch out["type"] {
	case "object":
		delete(out, "type")
	case "array":
		if items, ok := out["items"].(map[string]any); ok && items["type"] == "integer" {
			delete(out, "type")
			delete(out, "items")
		}
	}
	if props, ok := out["properties"].(map[string]any); ok {
		out["properties"] = reliableProperties(props)
	}
	if items, ok := out["items"]; ok {
		out["items"] = reliableNode(items)
	}
	return out
}
