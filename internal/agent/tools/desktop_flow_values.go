package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// Values passed between flow nodes.
//
// A capture names one value read from a node's checkpoint readback, for
// example the calculator result. Later nodes use it as {{name}} in text
// fields only: typed or set text, a checkpoint's expected text and a file
// check's expected content. Selectors, window identities, keys and paths
// never take placeholders, so a value read from the screen cannot choose a
// target or a command. A placeholder must name a capture made on every path
// to its node; that is checked before any input, and a missing value at run
// time stops the flow before the input that needs it.

// DesktopFlowCapture names a value read from the node's checkpoint.
type DesktopFlowCapture struct {
	Name    string               `json:"name" description:"Value name used later as {{name}}: lowercase letters, digits and _, starting with a letter, at most 32."`
	Pattern string               `json:"pattern,omitempty" description:"Optional RE2 pattern with exactly one group, matched against the whole readback (e.g. \"^Ekran değeri (.+)$\"). No match stops the flow before later input."`
	Number  *DesktopNumberFormat `json:"number,omitempty" description:"Parse the captured text as a localized number and pass it in plain form (69.104 with thousands \".\" becomes 69104; 8,5 with decimal \",\" becomes 8.5)."`
}

// DesktopNumberFormat states the separators the application displays. It is
// explicit because "8.638" is 8638 in Turkish and 8.638 in English.
type DesktopNumberFormat struct {
	Thousands string `json:"thousands,omitempty" description:"Grouping separator shown by the application: \".\", \",\", \" \", \"'\" or empty for none."`
	Decimal   string `json:"decimal" description:"Decimal separator shown by the application: \".\" or \",\"."`
}

// DesktopFileCheck is a read-only file-system check of a saved output.
type DesktopFileCheck struct {
	Path        string `json:"path" description:"Absolute path of the file, with its real extension (e.g. C:\\Users\\me\\Desktop\\deneme\\sonuc.txt). Placeholders are not allowed."`
	Condition   string `json:"condition" enum:"exists,absent,content,sha256,digest" description:"exists or absent, content (UTF-8 text equality), sha256 (expected hex digest), or digest (read the file's size and SHA-256 without a comparison)."`
	Expected    string `json:"expected,omitempty" description:"For content: the exact text; {{name}} captures allowed. For sha256: the hex digest."`
	LineEndings string `json:"line_endings,omitempty" enum:"normalize,exact" description:"For content: normalize (default) treats CRLF as LF and ignores one final newline; exact compares bytes."`
}

const (
	maxFlowCaptures      = 32
	maxCaptureValueBytes = 1024
	maxFileCheckBytes    = 1 << 20
)

var (
	captureNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	placeholderRef     = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]{0,31})\s*\}\}`)
	placeholderAny     = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	// Bidirectional marks some applications wrap around numbers.
	bidiMarks = strings.NewReplacer("\u200e", "", "\u200f", "", "\u202a", "", "\u202b", "", "\u202c", "", "\u202d", "", "\u202e", "", "\u2066", "", "\u2067", "", "\u2068", "", "\u2069", "")
)

func validateDesktopCapture(c *DesktopFlowCapture, q computer.AutomationRequest) error {
	if !captureNamePattern.MatchString(c.Name) {
		return fmt.Errorf("capture name must be lowercase letters, digits and _, starting with a letter, at most 32")
	}
	switch q.Condition {
	case "text", "value", "document_text", "file_name":
	default:
		return fmt.Errorf("capture reads a text readback; use checkpoint condition text, value, document_text or file_name")
	}
	if placeholderAny.MatchString(q.Expected) {
		return fmt.Errorf("a capturing checkpoint cannot itself use placeholders")
	}
	if q.Expected == "" && q.WaitMS != 0 {
		return fmt.Errorf("a capture without expected reads the current value and cannot wait; give expected to wait for a known value")
	}
	if q.Expected == "" && q.Condition == "file_name" {
		return fmt.Errorf("a file_name capture needs expected; read the current value with text, value or document_text")
	}
	if c.Pattern != "" {
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return fmt.Errorf("capture pattern is not valid RE2: %v", err)
		}
		if re.NumSubexp() != 1 {
			return fmt.Errorf("capture pattern needs exactly one group, got %d", re.NumSubexp())
		}
	}
	if c.Number != nil {
		if c.Number.Decimal != "." && c.Number.Decimal != "," {
			return fmt.Errorf("number.decimal must be \".\" or \",\"")
		}
		if !slices.Contains([]string{"", ".", ",", " ", "'"}, c.Number.Thousands) || c.Number.Thousands == c.Number.Decimal {
			return fmt.Errorf("number.thousands must be empty, \".\", \",\", \" \" or \"'\" and differ from decimal")
		}
	}
	return nil
}

// desktopFlowPlaceholderProbeText replaces placeholders with a neutral
// stand-in so the surrounding text is validated with the input contract.
func desktopFlowPlaceholderProbeText(text string) string {
	return placeholderRef.ReplaceAllString(text, "0")
}

// flowPlaceholderFields lists the text fields of a node that accept
// placeholders, keyed by their field path.
func flowPlaceholderFields(n DesktopFlowNode) map[string]string {
	fields := map[string]string{"checkpoint.expected": n.Checkpoint.Expected}
	if n.File != nil {
		fields["file.expected"] = n.File.Expected
	}
	for k, input := range desktopFlowInputs(n) {
		field := "input"
		if len(n.Inputs) > 0 {
			field = fmt.Sprintf("inputs[%d]", k)
		}
		fields[field+".text"] = input.Text
		fields[field+".automation.text"] = input.Automation.Text
	}
	return fields
}

// flowForbiddenPlaceholder finds a placeholder in a field that selects a
// target or command, where a read value must never go.
func flowForbiddenPlaceholder(n DesktopFlowNode) string {
	check := map[string]string{"checkpoint.name": n.Checkpoint.Name, "checkpoint.role": n.Checkpoint.Role, "checkpoint.element_id": n.Checkpoint.ElementID, "application": n.Application, "title": n.Title}
	if n.File != nil {
		check["file.path"] = n.File.Path
	}
	if n.Rename != nil {
		check["rename.old_name"], check["rename.new_name"], check["rename.expected_full_name"] = n.Rename.OldName, n.Rename.NewName, n.Rename.ExpectedFullName
	}
	for k, input := range desktopFlowInputs(n) {
		field := fmt.Sprintf("inputs[%d]", k)
		check[field+".key"], check[field+".modifiers"] = input.Key, input.Modifiers
		check[field+".automation.name"], check[field+".automation.role"], check[field+".automation.element_id"] = input.Automation.Name, input.Automation.Role, input.Automation.ElementID
	}
	keys := make([]string, 0, len(check))
	for key := range check {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if placeholderAny.MatchString(check[key]) {
			return key
		}
	}
	return ""
}

// validateDesktopFlowCaptures checks capture names and that every
// placeholder names a value captured on every path before its node. Nodes
// are in forward order (edges only point later), so one pass computes the
// values guaranteed at each node.
func validateDesktopFlowCaptures(nodes []DesktopFlowNode, ids map[string]int) error {
	defined := map[string]int{}
	for i, n := range nodes {
		if n.Capture == nil {
			continue
		}
		if j, exists := defined[n.Capture.Name]; exists {
			return flowError(i, n.ID, "capture.name", fmt.Sprintf("capture %q is already defined by node %q", n.Capture.Name, nodes[j].ID))
		}
		defined[n.Capture.Name] = i
	}
	if len(defined) > maxFlowCaptures {
		return flowError(-1, "", "nodes", fmt.Sprintf("a flow may capture at most %d values", maxFlowCaptures))
	}
	available := make([]map[string]bool, len(nodes))
	available[0] = map[string]bool{}
	for i, n := range nodes {
		if field := flowForbiddenPlaceholder(n); field != "" {
			return flowError(i, n.ID, field, "placeholders are allowed only in text, expected and file.expected fields; a read value never selects a target, key or path")
		}
		if available[i] == nil {
			// Unreachable from the entry node: it never runs.
			continue
		}
		fields := flowPlaceholderFields(n)
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if len(placeholderAny.FindAllString(fields[key], -1)) != len(placeholderRef.FindAllString(fields[key], -1)) {
				return flowError(i, n.ID, key, "malformed placeholder: write {{name}} with a lowercase capture name; {{ }} is reserved for captured values")
			}
			for _, match := range placeholderRef.FindAllStringSubmatch(fields[key], -1) {
				name := match[1]
				if _, ok := defined[name]; !ok {
					return flowError(i, n.ID, key, fmt.Sprintf("{{%s}} names no capture in this flow", name))
				}
				if !available[i][name] {
					return flowError(i, n.ID, key, fmt.Sprintf("{{%s}} is not captured on every path before this node", name))
				}
			}
		}
		out := map[string]bool{}
		for name := range available[i] {
			out[name] = true
		}
		if n.Capture != nil {
			out[n.Capture.Name] = true
		}
		for _, edge := range []string{n.Next, n.Then, n.Else} {
			j, ok := ids[edge]
			if edge == "" || !ok {
				continue
			}
			if available[j] == nil {
				available[j] = maps.Clone(out)
				continue
			}
			for name := range available[j] {
				if !out[name] {
					delete(available[j], name)
				}
			}
		}
	}
	return nil
}

// substituteFlowText replaces placeholders with captured values. A missing
// value is an error; nothing is guessed or left as literal braces.
func substituteFlowText(text string, values map[string]string) (string, error) {
	var missing string
	out := placeholderRef.ReplaceAllStringFunc(text, func(match string) string {
		name := placeholderRef.FindStringSubmatch(match)[1]
		value, ok := values[name]
		if !ok && missing == "" {
			missing = name
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("capture_missing: {{%s}} has no captured value on this run; nothing was sent", missing)
	}
	return out, nil
}

// substituteFlowNode returns n with every placeholder field filled in.
func substituteFlowNode(n DesktopFlowNode, values map[string]string) (DesktopFlowNode, error) {
	var err error
	fill := func(s string) string {
		if err != nil || !strings.Contains(s, "{{") {
			return s
		}
		var out string
		out, err = substituteFlowText(s, values)
		return out
	}
	n.Checkpoint.Expected = fill(n.Checkpoint.Expected)
	n.Input.Text = fill(n.Input.Text)
	n.Input.Automation.Text = fill(n.Input.Automation.Text)
	if len(n.Inputs) > 0 {
		inputs := slices.Clone(n.Inputs)
		for i := range inputs {
			inputs[i].Text = fill(inputs[i].Text)
			inputs[i].Automation.Text = fill(inputs[i].Automation.Text)
		}
		n.Inputs = inputs
	}
	if n.File != nil {
		file := *n.File
		file.Expected = fill(file.Expected)
		n.File = &file
	}
	return n, err
}

// captureFlowValue extracts the named value from an assert readback of the
// checkpoint window. The readback must be complete, from that window, and
// match the pattern; otherwise the value is not captured.
func captureFlowValue(content string, q computer.AutomationRequest, c *DesktopFlowCapture) (string, error) {
	var read struct {
		Actual    *string `json:"actual"`
		Truncated bool    `json:"actual_truncated"`
		WindowID  string  `json:"window_id"`
	}
	if len(content) > 8*1024 || json.Unmarshal([]byte(content), &read) != nil || read.Actual == nil || read.Truncated || read.WindowID != q.WindowID {
		return "", fmt.Errorf("capture_unreadable: %s needs one complete text readback from the checkpoint window; later input was not sent", c.Name)
	}
	value := *read.Actual
	if c.Pattern != "" {
		pattern, err := regexp.Compile(`\A(?:` + c.Pattern + `)\z`)
		if err != nil {
			return "", fmt.Errorf("capture_mismatch: invalid pattern for %s: %w", c.Name, err)
		}
		match := pattern.FindStringSubmatch(value)
		if match == nil {
			return "", fmt.Errorf("capture_mismatch: readback %q does not match the pattern of %s; later input was not sent", pipelineShort(value, 128), c.Name)
		}
		value = match[1]
	}
	if c.Number != nil {
		number, err := parseDisplayedNumber(value, *c.Number)
		if err != nil {
			return "", fmt.Errorf("capture_mismatch: %s: %v; later input was not sent", c.Name, err)
		}
		value = number
	}
	if value == "" || len(value) > maxCaptureValueBytes || !utf8.ValidString(value) {
		return "", fmt.Errorf("capture_mismatch: %s is empty, invalid or longer than %d bytes; later input was not sent", c.Name, maxCaptureValueBytes)
	}
	return value, nil
}

// parseDisplayedNumber converts a displayed number with explicit separators
// into plain digits with "." as decimal point. Misplaced group separators
// are refused instead of being read some other way.
func parseDisplayedNumber(text string, format DesktopNumberFormat) (string, error) {
	text = strings.TrimSpace(bidiMarks.Replace(text))
	text = strings.ReplaceAll(text, "\u2212", "-")
	if format.Thousands == " " {
		text = strings.NewReplacer("\u00a0", " ", "\u202f", " ").Replace(text)
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, fraction, hasFraction := strings.Cut(text, format.Decimal)
	if hasFraction && (fraction == "" || strings.ContainsAny(fraction, ".,' ")) {
		return "", fmt.Errorf("%q is not a number in this format", text)
	}
	groups := []string{whole}
	if format.Thousands != "" {
		groups = strings.Split(whole, format.Thousands)
	}
	for i, group := range groups {
		if group == "" || strings.Trim(group, "0123456789") != "" || (i > 0 && len(group) != 3) || (len(groups) > 1 && i == 0 && len(group) > 3) {
			return "", fmt.Errorf("%q is not a number in this format", text)
		}
	}
	for _, r := range fraction {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("%q is not a number in this format", text)
		}
	}
	out := strings.Join(groups, "")
	if hasFraction {
		out += "." + fraction
	}
	if negative {
		out = "-" + out
	}
	return out, nil
}

func validateDesktopFileCheck(f *DesktopFileCheck) error {
	if f == nil {
		return fmt.Errorf("file_check requires file with path and condition")
	}
	if !filepath.IsAbs(f.Path) || filepath.Clean(f.Path) != f.Path || strings.ContainsRune(f.Path, 0) {
		return fmt.Errorf("file.path must be an absolute, clean path")
	}
	if f.LineEndings != "" && f.LineEndings != "normalize" && f.LineEndings != "exact" {
		return fmt.Errorf("file.line_endings must be normalize or exact")
	}
	switch f.Condition {
	case "exists", "absent", "digest":
		if f.Expected != "" || f.LineEndings != "" {
			return fmt.Errorf("exists/absent/digest take no expected or line_endings")
		}
	case "content":
		if len(f.Expected) > maxFileCheckBytes {
			return fmt.Errorf("file.expected exceeds %d bytes", maxFileCheckBytes)
		}
	case "sha256":
		if decoded, err := hex.DecodeString(f.Expected); err != nil || len(decoded) != sha256.Size || f.LineEndings != "" {
			return fmt.Errorf("sha256 requires a 64-digit hex expected digest")
		}
	default:
		return fmt.Errorf("file.condition must be exists, absent, content, sha256 or digest")
	}
	return nil
}

// fileSnapshot reads a regular file once. Content checks are bounded before
// and during reading, and their digest describes those same bytes.
func fileSnapshot(ctx context.Context, path string, content bool) (int64, string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, "", nil, err
	}
	if !info.Mode().IsRegular() {
		return 0, "", nil, fmt.Errorf("%s is not a regular file", path)
	}
	if content && info.Size() > maxFileCheckBytes {
		return 0, "", nil, fmt.Errorf("file_too_large: content exceeds %d bytes; use sha256", maxFileCheckBytes)
	}
	hash := sha256.New()
	var data bytes.Buffer
	var destination io.Writer = hash
	var source io.Reader = file
	if content {
		destination = io.MultiWriter(hash, &data)
		source = io.LimitReader(file, maxFileCheckBytes+1)
	}
	size, err := io.CopyBuffer(destination, &flowContextReader{ctx: ctx, reader: source}, make([]byte, 32*1024))
	if err != nil {
		return 0, "", nil, err
	}
	if content && size > maxFileCheckBytes {
		return 0, "", nil, fmt.Errorf("file_too_large: content exceeds %d bytes; use sha256", maxFileCheckBytes)
	}
	after, err := file.Stat()
	if err != nil {
		return 0, "", nil, err
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(info, current) || size != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || current.Size() != after.Size() || !current.ModTime().Equal(after.ModTime()) {
		return 0, "", nil, fmt.Errorf("file_changed: file identity or contents changed during the check; read it again")
	}
	return size, hex.EncodeToString(hash.Sum(nil)), data.Bytes(), ctx.Err()
}

// flowContextReader observes cancellation between bounded reads, including
// a cancellation raised while the underlying read was in progress.
type flowContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *flowContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

func normalizeFileText(text string) string {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSuffix(text, "\n")
}

// runDesktopFileCheck performs one read-only file check and reports the
// evidence: existence, size, digest and, for content, the first differing
// line. It never prints more than a short preview of the file.
func runDesktopFileCheck(ctx context.Context, f DesktopFileCheck) fantasy.ToolResponse {
	if err := ctx.Err(); err != nil {
		return fantasy.NewTextErrorResponse("canceled: file check was not run")
	}
	evidence := map[string]any{"path": f.Path, "condition": f.Condition, "source": "file_system"}
	info, statErr := os.Stat(f.Path)
	if f.Condition == "absent" {
		// A dangling link is still a directory entry and occupies a rename
		// candidate. Absence must not follow it to a missing target.
		info, statErr = os.Lstat(f.Path)
	}
	exists := statErr == nil
	evidence["exists"] = exists
	if statErr != nil && !os.IsNotExist(statErr) {
		evidence["error"] = statErr.Error()
	}
	report := func(passed bool, code, reason string) fantasy.ToolResponse {
		evidence["passed"] = passed
		data, _ := json.Marshal(evidence)
		if passed {
			return fantasy.NewTextResponse(string(data))
		}
		return fantasy.NewTextErrorResponse(code + ": " + reason + "; no input was sent by this check. " + string(data))
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return report(false, "file_unreadable", "the file could not be inspected")
	}
	switch f.Condition {
	case "absent":
		return report(!exists && os.IsNotExist(statErr), "file_present", "the file exists")
	case "exists":
		if exists && !info.Mode().IsRegular() {
			return report(false, "file_mismatch", "the path is not a regular file")
		}
		return report(exists, "file_missing", "the file does not exist")
	}
	if !exists {
		return report(false, "file_missing", "the file does not exist")
	}
	if !info.Mode().IsRegular() {
		return report(false, "file_mismatch", "the path is not a regular file")
	}
	size, digest, data, err := fileSnapshot(ctx, f.Path, f.Condition == "content")
	if err != nil {
		evidence["error"] = err.Error()
		if ctx.Err() != nil {
			return report(false, "canceled", "file check was interrupted")
		}
		if strings.HasPrefix(err.Error(), "file_too_large:") {
			return report(false, "file_mismatch", err.Error())
		}
		if strings.HasPrefix(err.Error(), "file_changed:") {
			return report(false, "file_changed", "file changed during verification")
		}
		return report(false, "file_unreadable", "the file could not be read")
	}
	evidence["size"], evidence["sha256"] = size, digest
	if f.Condition == "digest" {
		return report(true, "", "")
	}
	if f.Condition == "sha256" {
		return report(strings.EqualFold(digest, f.Expected), "file_mismatch", "the digest differs")
	}
	if !utf8.Valid(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))) {
		return report(false, "file_mismatch", "the file is not readable UTF-8 text")
	}
	actual, expected := string(data), f.Expected
	if f.LineEndings != "exact" {
		actual, expected = normalizeFileText(actual), normalizeFileText(expected)
	}
	if actual == expected {
		return report(true, "", "")
	}
	actualLines, expectedLines := strings.Split(actual, "\n"), strings.Split(expected, "\n")
	for i := range max(len(actualLines), len(expectedLines)) {
		var a, e string
		if i < len(actualLines) {
			a = actualLines[i]
		}
		if i < len(expectedLines) {
			e = expectedLines[i]
		}
		if a != e {
			evidence["first_difference"] = map[string]any{"line": i + 1, "actual": pipelineShort(a, 200), "expected": pipelineShort(e, 200)}
			break
		}
	}
	return report(false, "file_mismatch", "the content differs")
}

// desktopFlowReadValue reads one control's current text, value or document
// text with a single find, for a capture that has no expected value. The
// control must resolve uniquely in the checkpoint window and its text must
// be complete; the response has the shape of an assert readback.
func desktopFlowReadValue(ctx context.Context, q computer.AutomationRequest, child desktopFlowChild) (fantasy.ToolResponse, error) {
	selector := computer.AutomationRequest{WindowID: q.WindowID, ElementID: q.ElementID, Name: q.Name, Role: q.Role}
	e, r, err := desktopFlowTarget(ctx, selector, child)
	if desktopRecipeStopped(r, err) {
		return r, err
	}
	var actual string
	complete := false
	switch q.Condition {
	case "text":
		actual, complete = e.Name, true
	case "value":
		actual, complete = e.Value, e.ValueAvailable && !e.ValueTruncated
	case "document_text":
		actual, complete = e.Text, e.TextAvailable && !e.TextTruncated
	}
	if !complete {
		return fantasy.NewTextErrorResponse("capture_unreadable: the control does not expose a complete " + q.Condition + " readback; later input was not sent"), nil
	}
	data, _ := json.Marshal(map[string]any{"passed": true, "actual": actual, "condition": q.Condition, "window_id": q.WindowID, "element_id": e.ID, "source": "find"})
	return fantasy.NewTextResponse(string(data)), nil
}
