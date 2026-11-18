package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm/schema"
	"github.com/kaptinlin/jsonschema"
)

const (
	// maxResultSchemaBytes bounds the schema a caller may attach to a
	// sub-agent run. The schema is repeated to the sub-agent on every retry.
	maxResultSchemaBytes = 16 * 1024
	// maxResultSchemaRetries is how many times a sub-agent whose reply does
	// not match the schema is asked to correct it, so a run makes at most
	// maxResultSchemaRetries+1 attempts.
	maxResultSchemaRetries = 2
)

// resultSchema is a compiled JSON Schema a sub-agent's final reply must
// satisfy. It turns a free-text report into data a parent can aggregate:
// ten sub-agents that each answer in the same shape can be merged without
// reading ten different essays.
type resultSchema struct {
	raw       string
	validator *jsonschema.Schema
}

// compileResultSchema checks that the schema is an object, is small enough
// and compiles. It reports the problem in words the calling model can act on.
func compileResultSchema(doc map[string]any) (*resultSchema, error) {
	if len(doc) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("result_schema is not valid JSON: %w", err)
	}
	if len(data) > maxResultSchemaBytes {
		return nil, fmt.Errorf("result_schema is %d bytes; keep it under %d", len(data), maxResultSchemaBytes)
	}
	validator, err := jsonschema.NewCompiler().Compile(data)
	if err != nil {
		return nil, fmt.Errorf("result_schema is not a valid JSON Schema: %w", err)
	}
	return &resultSchema{raw: string(data), validator: validator}, nil
}

// check validates a sub-agent reply. It returns the reply as compact JSON
// when it matches, or the problems found, one per line, when it does not.
func (s *resultSchema) check(reply string) (clean string, problems string) {
	body, ok := extractJSON(reply)
	if !ok {
		return "", "The reply contains no JSON value. Reply with the JSON value only."
	}
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return "", "The reply is not valid JSON: " + err.Error()
	}
	if result := s.validator.Validate(value); !result.IsValid() {
		return "", strings.Join(schema.ValidationMessages(result), "\n")
	}
	compact, err := json.Marshal(value)
	if err != nil {
		return body, ""
	}
	return string(compact), ""
}

// retryPrompt tells the sub-agent exactly what was wrong with its reply.
func (s *resultSchema) retryPrompt(problems string) string {
	return "Your final reply did not match the required result schema.\n\nProblems:\n" + problems +
		"\n\nSchema:\n" + s.raw +
		"\n\nReply again with only the corrected JSON value: no prose, no code fence. " +
		"Do not redo work you have finished; just fix the shape of the answer."
}

// instruction is appended to the sub-agent's task so it answers in the
// right shape the first time.
func (s *resultSchema) instruction() string {
	return "\n\nYour final reply must be only a JSON value that matches this JSON Schema: no prose, no code fence.\n" + s.raw
}

// extractJSON finds the JSON value in a reply: the whole reply, the body of
// a code fence, or the outermost object or array when prose surrounds it.
func extractJSON(reply string) (string, bool) {
	text := strings.TrimSpace(reply)
	if text == "" {
		return "", false
	}
	if json.Valid([]byte(text)) {
		return text, true
	}
	if start := strings.Index(text, "```"); start >= 0 {
		inner := text[start+3:]
		if nl := strings.IndexByte(inner, '\n'); nl >= 0 {
			inner = inner[nl+1:]
		}
		if end := strings.Index(inner, "```"); end >= 0 {
			if body := strings.TrimSpace(inner[:end]); json.Valid([]byte(body)) {
				return body, true
			}
		}
	}
	for _, pair := range [][2]byte{{'{', '}'}, {'[', ']'}} {
		start, end := strings.IndexByte(text, pair[0]), strings.LastIndexByte(text, pair[1])
		if start >= 0 && end > start {
			if body := text[start : end+1]; json.Valid([]byte(body)) {
				return body, true
			}
		}
	}
	return "", false
}
