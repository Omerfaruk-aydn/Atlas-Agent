// Package clibridge turns installed command-line programs into bounded,
// validated operations. A profile declares one executable, the operations it
// may perform and the typed parameters each operation accepts. Parameters
// become literal argv elements only through documented placeholders: no
// shell, template language or script is ever evaluated.
//
// The package performs no execution. Callers run prepared argv through the
// ordinary guarded command path so that hooks, permissions, task ownership,
// execution policies and background jobs keep their existing behavior.
package clibridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	// ProfileKind identifies an authored, reviewable profile document.
	ProfileKind = "atlas.cli-bridge.profile"
	// DraftKind identifies a help-derived draft. Drafts are never loaded
	// as active profiles.
	DraftKind = "atlas.cli-bridge.draft"
	// InstalledKind identifies the stored envelope written by Import.
	InstalledKind = "atlas.cli-bridge.installed"
	// ProfileVersion is the only supported profile format version.
	ProfileVersion = 1

	maxProfileBytes    = 256 * 1024
	maxOperations      = 32
	maxParams          = 32
	maxTemplateArgs    = 64
	maxArgBytes        = 4096
	maxDescription     = 1024
	maxEnvNames        = 32
	maxEnumValues      = 64
	maxListItems       = 64
	maxExtensions      = 16
	defaultTimeout     = 120
	maxTimeout         = 3600
	defaultOutputBytes = 64 * 1024
	minOutputBytes     = 1024
	maxOutputBytes     = 1024 * 1024
	defaultMaxLength   = 1024
	maxSafeInteger     = 1 << 53
)

// Parameter types.
const (
	TypeString     = "string"
	TypeInteger    = "integer"
	TypeNumber     = "number"
	TypeBoolean    = "boolean"
	TypeEnum       = "enum"
	TypeInputFile  = "input_file"
	TypeOutputFile = "output_file"
	TypeList       = "list"
)

var (
	idPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	paramPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	envPattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	sha256Pattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	extensionPattern = regexp.MustCompile(`^\.[A-Za-z0-9][A-Za-z0-9._-]{0,15}$`)
	listItemTypes    = []string{TypeString, TypeInteger, TypeNumber, TypeEnum, TypeInputFile}
)

// Profile is a versioned contract for one executable.
type Profile struct {
	Kind        string      `json:"kind"`
	Version     int         `json:"version"`
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Executable  Executable  `json:"executable"`
	Env         []string    `json:"env,omitempty"`
	Operations  []Operation `json:"operations"`
}

// Executable names the program. Name is resolved through PATH once, when
// the profile is imported or explicitly re-pinned; Path names an absolute
// file. SHA256 optionally pins the expected content.
type Executable struct {
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// Operation is one supported invocation.
type Operation struct {
	ID                string              `json:"id"`
	Description       string              `json:"description"`
	Args              []string            `json:"args"`
	Params            []Param             `json:"params,omitempty"`
	WorkingDir        string              `json:"working_dir,omitempty"`
	TimeoutSeconds    int                 `json:"timeout_seconds,omitempty"`
	MaxOutputBytes    int                 `json:"max_output_bytes,omitempty"`
	ExpectedExitCodes []int               `json:"expected_exit_codes,omitempty"`
	Outputs           []OutputExpectation `json:"outputs,omitempty"`
	StdoutContains    string              `json:"stdout_contains,omitempty"`
	Background        bool                `json:"background,omitempty"`
}

// Param is one typed value accepted from the caller.
type Param struct {
	Name             string          `json:"name"`
	Type             string          `json:"type"`
	Description      string          `json:"description,omitempty"`
	Required         bool            `json:"required,omitempty"`
	Default          json.RawMessage `json:"default,omitempty"`
	Flag             string          `json:"flag,omitempty"`
	Min              *float64        `json:"min,omitempty"`
	Max              *float64        `json:"max,omitempty"`
	MinLength        int             `json:"min_length,omitempty"`
	MaxLength        int             `json:"max_length,omitempty"`
	Pattern          string          `json:"pattern,omitempty"`
	Values           []string        `json:"values,omitempty"`
	Extensions       []string        `json:"extensions,omitempty"`
	AllowExternal    bool            `json:"allow_external,omitempty"`
	Overwrite        bool            `json:"overwrite,omitempty"`
	AllowLeadingDash bool            `json:"allow_leading_dash,omitempty"`
	Secret           bool            `json:"secret,omitempty"`
	ItemType         string          `json:"item_type,omitempty"`
	MinItems         int             `json:"min_items,omitempty"`
	MaxItems         int             `json:"max_items,omitempty"`
}

// OutputExpectation is checked against the observed file after a run.
type OutputExpectation struct {
	Param    string `json:"param"`
	MinBytes int64  `json:"min_bytes,omitempty"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

// NormalizeText accepts the encodings Windows editors and shells commonly
// write -- UTF-8 with a byte order mark and UTF-16 with one -- and returns
// plain UTF-8. Anything else is returned unchanged.
func NormalizeText(data []byte) []byte {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:]
	case len(data)%2 == 0 && (bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF})):
		little := data[0] == 0xFF
		units := make([]uint16, 0, len(data)/2-1)
		for i := 2; i+1 < len(data); i += 2 {
			if little {
				units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
			} else {
				units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
			}
		}
		return []byte(string(utf16.Decode(units)))
	}
	return data
}

// Decode accepts exactly one JSON value and rejects unknown fields.
func Decode(data []byte, value any) error {
	data = NormalizeText(data)
	if len(data) > maxProfileBytes {
		return fmt.Errorf("profile exceeds %d bytes", maxProfileBytes)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("decode profile: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("profile must contain exactly one JSON value")
	}
	return nil
}

// ParseProfile decodes and validates an authored profile. A draft is
// rejected with an explanation, because help output never authorizes an
// operation by itself.
func ParseProfile(data []byte) (Profile, error) {
	data = NormalizeText(data)
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.Kind == DraftKind {
		return Profile{}, fmt.Errorf("this is a help-derived draft; author operations in a %q document and review every argument mapping before importing", ProfileKind)
	}
	var p Profile
	if err := Decode(data, &p); err != nil {
		return Profile{}, err
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// Hash is the SHA-256 of the canonical encoding of a validated profile.
func (p Profile) Hash() string {
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Operation returns the named operation.
func (p Profile) Operation(id string) (Operation, error) {
	for _, op := range p.Operations {
		if op.ID == id {
			return op, nil
		}
	}
	names := make([]string, 0, len(p.Operations))
	for _, op := range p.Operations {
		names = append(names, op.ID)
	}
	return Operation{}, fmt.Errorf("profile %s has no operation %q; available: %s", p.ID, id, strings.Join(names, ", "))
}

// Validate checks every field strictly. Fields that do not apply to a
// parameter type are rejected rather than ignored, so a profile never
// appears to constrain something it does not.
func (p Profile) Validate() error {
	if p.Kind != ProfileKind {
		return fmt.Errorf("kind must be %q", ProfileKind)
	}
	if p.Version != ProfileVersion {
		return fmt.Errorf("unsupported profile version %d; only version %d is supported", p.Version, ProfileVersion)
	}
	if !idPattern.MatchString(p.ID) {
		return fmt.Errorf("profile id %q must match %s", p.ID, idPattern)
	}
	if err := validText("description", p.Description, true); err != nil {
		return err
	}
	if err := p.Executable.validate(); err != nil {
		return err
	}
	if len(p.Env) > maxEnvNames {
		return fmt.Errorf("at most %d environment names", maxEnvNames)
	}
	seenEnv := map[string]bool{}
	for _, name := range p.Env {
		if !envPattern.MatchString(name) {
			return fmt.Errorf("env entries are variable names only (no values): %q", name)
		}
		if seenEnv[strings.ToUpper(name)] {
			return fmt.Errorf("duplicate env name %q", name)
		}
		seenEnv[strings.ToUpper(name)] = true
	}
	if len(p.Operations) == 0 || len(p.Operations) > maxOperations {
		return fmt.Errorf("profile requires 1-%d operations", maxOperations)
	}
	seen := map[string]bool{}
	for _, op := range p.Operations {
		if seen[op.ID] {
			return fmt.Errorf("duplicate operation id %q", op.ID)
		}
		seen[op.ID] = true
		if err := op.validate(); err != nil {
			return fmt.Errorf("operation %q: %w", op.ID, err)
		}
	}
	return nil
}

func (e Executable) validate() error {
	if (e.Name == "") == (e.Path == "") {
		return fmt.Errorf("executable requires exactly one of name or path")
	}
	if e.Name != "" {
		if len(e.Name) > 128 || strings.ContainsAny(e.Name, "/\\\x00:") || e.Name == "." || e.Name == ".." || strings.TrimSpace(e.Name) != e.Name {
			return fmt.Errorf("executable name must be a bare program name without directories")
		}
	}
	if e.Path != "" {
		if len(e.Path) > 4096 || strings.ContainsAny(e.Path, "\x00\r\n") || !filepath.IsAbs(e.Path) {
			return fmt.Errorf("executable path must be absolute")
		}
	}
	if e.SHA256 != "" && !sha256Pattern.MatchString(e.SHA256) {
		return fmt.Errorf("executable sha256 must be 64 lowercase hex characters")
	}
	return nil
}

func validText(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > maxDescription || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s must be valid UTF-8 up to %d bytes", field, maxDescription)
	}
	return nil
}

// Limits returns the effective timeout, output and exit expectations.
func (op Operation) Limits() (timeout, output int, exits []int) {
	timeout, output, exits = op.TimeoutSeconds, op.MaxOutputBytes, op.ExpectedExitCodes
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if output == 0 {
		output = defaultOutputBytes
	}
	if len(exits) == 0 {
		exits = []int{0}
	}
	return timeout, output, exits
}

// Param returns the named parameter.
func (op Operation) Param(name string) (Param, bool) {
	for _, p := range op.Params {
		if p.Name == name {
			return p, true
		}
	}
	return Param{}, false
}

func (op Operation) validate() error {
	if !idPattern.MatchString(op.ID) {
		return fmt.Errorf("operation id must match %s", idPattern)
	}
	if err := validText("description", op.Description, true); err != nil {
		return err
	}
	if len(op.Args) > maxTemplateArgs {
		return fmt.Errorf("at most %d argument templates", maxTemplateArgs)
	}
	if len(op.Params) > maxParams {
		return fmt.Errorf("at most %d params", maxParams)
	}
	if op.WorkingDir != "" {
		if !filepath.IsLocal(filepath.FromSlash(op.WorkingDir)) || strings.ContainsAny(op.WorkingDir, "\x00\r\n") {
			return fmt.Errorf("working_dir must be a project-relative directory")
		}
	}
	if op.TimeoutSeconds < 0 || op.TimeoutSeconds > maxTimeout {
		return fmt.Errorf("timeout_seconds must be 1-%d", maxTimeout)
	}
	if op.MaxOutputBytes != 0 && (op.MaxOutputBytes < minOutputBytes || op.MaxOutputBytes > maxOutputBytes) {
		return fmt.Errorf("max_output_bytes must be %d-%d", minOutputBytes, maxOutputBytes)
	}
	if len(op.ExpectedExitCodes) > 16 {
		return fmt.Errorf("at most 16 expected exit codes")
	}
	for i, code := range op.ExpectedExitCodes {
		if code < 0 || code > 255 || slices.Contains(op.ExpectedExitCodes[:i], code) {
			return fmt.Errorf("expected_exit_codes must be unique values 0-255")
		}
	}
	if len(op.StdoutContains) > 1024 || strings.ContainsRune(op.StdoutContains, 0) {
		return fmt.Errorf("stdout_contains must be at most 1024 bytes")
	}
	params := map[string]Param{}
	for _, p := range op.Params {
		if _, ok := params[p.Name]; ok {
			return fmt.Errorf("duplicate param %q", p.Name)
		}
		if err := p.validate(); err != nil {
			return fmt.Errorf("param %q: %w", p.Name, err)
		}
		params[p.Name] = p
	}
	used := map[string]bool{}
	for i, arg := range op.Args {
		tpl, err := parseTemplate(arg)
		if err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
		if tpl.param == "" {
			continue
		}
		p, ok := params[tpl.param]
		if !ok {
			return fmt.Errorf("args[%d] references undeclared param {%s}", i, tpl.param)
		}
		if used[tpl.param] {
			return fmt.Errorf("param {%s} is referenced more than once", tpl.param)
		}
		used[tpl.param] = true
		if tpl.embedded() {
			if p.Type == TypeList || p.Type == TypeBoolean || p.Flag != "" {
				return fmt.Errorf("args[%d]: %s params with a flag, booleans and lists must occupy a whole argument", i, p.Name)
			}
			if !p.Required && len(p.Default) == 0 {
				return fmt.Errorf("args[%d]: embedded param {%s} must be required or have a default", i, p.Name)
			}
		}
	}
	for name := range params {
		if !used[name] {
			return fmt.Errorf("param %q is declared but never placed in args", name)
		}
	}
	seenOutputs := map[string]bool{}
	for _, out := range op.Outputs {
		p, ok := params[out.Param]
		if !ok || p.Type != TypeOutputFile {
			return fmt.Errorf("outputs entry %q must name an output_file param", out.Param)
		}
		if seenOutputs[out.Param] {
			return fmt.Errorf("duplicate outputs entry %q", out.Param)
		}
		seenOutputs[out.Param] = true
		if out.MinBytes < 0 || out.MaxBytes < 0 || out.MaxBytes != 0 && out.MaxBytes < out.MinBytes {
			return fmt.Errorf("outputs entry %q has invalid byte bounds", out.Param)
		}
	}
	return nil
}

// applicable lists the optional fields each type accepts.
var applicable = map[string][]string{
	TypeString:     {"flag", "min_length", "max_length", "pattern", "allow_leading_dash", "secret"},
	TypeInteger:    {"flag", "min", "max"},
	TypeNumber:     {"flag", "min", "max"},
	TypeBoolean:    {"flag"},
	TypeEnum:       {"flag", "values"},
	TypeInputFile:  {"flag", "extensions", "allow_external"},
	TypeOutputFile: {"flag", "extensions", "overwrite"},
	TypeList:       {"flag", "item_type", "min_items", "max_items", "min", "max", "min_length", "max_length", "pattern", "values", "extensions", "allow_external", "allow_leading_dash"},
}

func (p Param) setFields() []string {
	var out []string
	add := func(name string, set bool) {
		if set {
			out = append(out, name)
		}
	}
	add("flag", p.Flag != "")
	add("min", p.Min != nil)
	add("max", p.Max != nil)
	add("min_length", p.MinLength != 0)
	add("max_length", p.MaxLength != 0)
	add("pattern", p.Pattern != "")
	add("values", p.Values != nil)
	add("extensions", p.Extensions != nil)
	add("allow_external", p.AllowExternal)
	add("overwrite", p.Overwrite)
	add("allow_leading_dash", p.AllowLeadingDash)
	add("secret", p.Secret)
	add("item_type", p.ItemType != "")
	add("min_items", p.MinItems != 0)
	add("max_items", p.MaxItems != 0)
	return out
}

func (p Param) validate() error {
	if !paramPattern.MatchString(p.Name) {
		return fmt.Errorf("name must match %s", paramPattern)
	}
	allowed, ok := applicable[p.Type]
	if !ok {
		return fmt.Errorf("unknown type %q", p.Type)
	}
	for _, field := range p.setFields() {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("%s does not apply to type %s", field, p.Type)
		}
	}
	if err := validText("description", p.Description, false); err != nil {
		return err
	}
	if p.Required && len(p.Default) > 0 {
		return fmt.Errorf("a required param cannot have a default")
	}
	if p.Flag != "" && (len(p.Flag) > 128 || !strings.HasPrefix(p.Flag, "-") || strings.ContainsAny(p.Flag, "\x00{} \t\r\n")) {
		return fmt.Errorf("flag must be a literal option starting with '-'")
	}
	if p.Type == TypeBoolean && p.Flag == "" {
		return fmt.Errorf("boolean params require a flag; the flag is emitted only when true")
	}
	itemType := p.Type
	if p.Type == TypeList {
		if !slices.Contains(listItemTypes, p.ItemType) {
			return fmt.Errorf("item_type must be one of %s", strings.Join(listItemTypes, ", "))
		}
		itemType = p.ItemType
		if p.MinItems < 0 || p.MaxItems < 1 || p.MaxItems > maxListItems || p.MinItems > p.MaxItems {
			return fmt.Errorf("lists require max_items 1-%d and 0 <= min_items <= max_items", maxListItems)
		}
		if p.Required && p.MinItems == 0 {
			return fmt.Errorf("a required list needs min_items of at least 1")
		}
		// Item constraints must apply to the declared item type.
		item := p
		item.Type, item.ItemType, item.MinItems, item.MaxItems, item.Flag, item.Default = p.ItemType, "", 0, 0, "", nil
		for _, field := range item.setFields() {
			if !slices.Contains(applicable[p.ItemType], field) {
				return fmt.Errorf("%s does not apply to list items of type %s", field, p.ItemType)
			}
		}
	}
	switch itemType {
	case TypeString:
		if p.MinLength < 0 || p.MaxLength < 0 || p.MaxLength > maxArgBytes || p.MaxLength != 0 && p.MinLength > p.MaxLength {
			return fmt.Errorf("lengths must satisfy 0 <= min_length <= max_length <= %d", maxArgBytes)
		}
		if p.Pattern != "" {
			if len(p.Pattern) > 512 {
				return fmt.Errorf("pattern exceeds 512 bytes")
			}
			if _, err := regexp.Compile(p.Pattern); err != nil {
				return fmt.Errorf("invalid pattern: %w", err)
			}
		}
		if p.Secret && p.Type == TypeString && len(p.Default) > 0 {
			return fmt.Errorf("secret params cannot carry a default value")
		}
	case TypeInteger, TypeNumber:
		if p.Min == nil || p.Max == nil {
			return fmt.Errorf("%s params require explicit min and max", itemType)
		}
		for _, bound := range []float64{*p.Min, *p.Max} {
			if math.IsNaN(bound) || math.IsInf(bound, 0) {
				return fmt.Errorf("bounds must be finite")
			}
			if itemType == TypeInteger && (bound != math.Trunc(bound) || math.Abs(bound) > maxSafeInteger) {
				return fmt.Errorf("integer bounds must be integral and within +/-2^53")
			}
		}
		if *p.Min > *p.Max {
			return fmt.Errorf("min exceeds max")
		}
	case TypeEnum:
		if len(p.Values) == 0 || len(p.Values) > maxEnumValues {
			return fmt.Errorf("enum requires 1-%d values", maxEnumValues)
		}
		for i, v := range p.Values {
			if v == "" || len(v) > 256 || strings.ContainsRune(v, 0) || slices.Contains(p.Values[:i], v) {
				return fmt.Errorf("enum values must be unique non-empty strings up to 256 bytes")
			}
		}
	case TypeInputFile, TypeOutputFile:
		if len(p.Extensions) > maxExtensions {
			return fmt.Errorf("at most %d extensions", maxExtensions)
		}
		for i, ext := range p.Extensions {
			if !extensionPattern.MatchString(ext) || slices.ContainsFunc(p.Extensions[:i], func(other string) bool { return strings.EqualFold(other, ext) }) {
				return fmt.Errorf("extensions must be unique values such as .mp4")
			}
		}
	}
	if len(p.Default) > 0 {
		if p.Type == TypeInputFile || p.Type == TypeOutputFile || p.ItemType == TypeInputFile {
			return fmt.Errorf("file params cannot have defaults; the caller must name every file")
		}
		var values []string
		if err := decodeDefault(p, &values); err != nil {
			return fmt.Errorf("default: %w", err)
		}
		if _, err := p.parseValues(values); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

// decodeDefault converts a typed JSON default into the string form callers
// use, so defaults pass through exactly the same checks as caller values.
func decodeDefault(p Param, out *[]string) error {
	scalar := func(raw json.RawMessage, itemType string) (string, error) {
		switch itemType {
		case TypeString, TypeEnum:
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return "", fmt.Errorf("must be a JSON string")
			}
			return s, nil
		case TypeInteger, TypeNumber:
			var n json.Number
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			if err := d.Decode(&n); err != nil {
				return "", fmt.Errorf("must be a JSON number")
			}
			return n.String(), nil
		case TypeBoolean:
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				return "", fmt.Errorf("must be a JSON boolean")
			}
			if b {
				return "true", nil
			}
			return "false", nil
		}
		return "", fmt.Errorf("type %s has no default", itemType)
	}
	if p.Type == TypeList {
		var items []json.RawMessage
		if err := json.Unmarshal(p.Default, &items); err != nil {
			return fmt.Errorf("must be a JSON array")
		}
		for _, item := range items {
			s, err := scalar(item, p.ItemType)
			if err != nil {
				return err
			}
			*out = append(*out, s)
		}
		return nil
	}
	s, err := scalar(p.Default, p.Type)
	if err != nil {
		return err
	}
	*out = []string{s}
	return nil
}

// template is one parsed argument template element.
type template struct {
	prefix, suffix string
	param          string
}

func (t template) embedded() bool { return t.param != "" && (t.prefix != "" || t.suffix != "") }

// parseTemplate accepts literal text with at most one {name} placeholder.
// "{{" and "}}" denote literal braces. Nothing else is interpreted.
func parseTemplate(arg string) (template, error) {
	if len(arg) > maxArgBytes || strings.ContainsRune(arg, 0) || !utf8.ValidString(arg) {
		return template{}, fmt.Errorf("argument must be valid UTF-8 up to %d bytes without NUL", maxArgBytes)
	}
	var t template
	var b strings.Builder
	found := false
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		switch {
		case c == '{' && i+1 < len(arg) && arg[i+1] == '{':
			b.WriteByte('{')
			i++
		case c == '}' && i+1 < len(arg) && arg[i+1] == '}':
			b.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(arg[i+1:], '}')
			if end < 0 {
				return template{}, fmt.Errorf("unterminated placeholder in %q", arg)
			}
			name := arg[i+1 : i+1+end]
			if !paramPattern.MatchString(name) {
				return template{}, fmt.Errorf("invalid placeholder {%s}; use {{ and }} for literal braces", name)
			}
			if found {
				return template{}, fmt.Errorf("an argument may contain at most one placeholder")
			}
			found = true
			t.prefix = b.String()
			b.Reset()
			t.param = name
			i += end + 1
		case c == '}':
			return template{}, fmt.Errorf("unmatched '}' in %q; use }} for a literal brace", arg)
		default:
			b.WriteByte(c)
		}
	}
	if found {
		t.suffix = b.String()
	} else {
		t.prefix = b.String()
	}
	return t, nil
}
