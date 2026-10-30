package webverify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// SpecFile is the project file that describes how to verify the product,
// relative to the project root. The agent writes it next to .atlas/work.json
// when the product has user flows worth driving.
const SpecFile = ".atlas/verify.json"

// Limits keep a spec written by accident from running for minutes.
const (
	maxSpecBytes = 128 << 10
	maxFlows     = 20
	maxSteps     = 30
	maxPages     = 5
	maxTextLen   = 2048
)

// Step actions a flow may use.
const (
	StepNavigate = "navigate"
	StepClick    = "click"
	StepType     = "type"
	StepKey      = "key"
	StepAssert   = "assert"
)

// Step is one browser action of a flow.
type Step struct {
	Action string `json:"action"`
	// URL is a page path or local address, for navigate.
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Role     string `json:"role,omitempty"`
	Name     string `json:"name,omitempty"`
	Label    string `json:"label,omitempty"`
	// Text is typed into the field, for type.
	Text string `json:"text,omitempty"`
	// Key is the key name, for key.
	Key string `json:"key,omitempty"`
	// Condition and Expected are the assertion, for assert.
	Condition string `json:"condition,omitempty"`
	Expected  string `json:"expected,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// Flow is a user journey: a named list of steps ending in assertions.
type Flow struct {
	Name string `json:"name"`
	// Source says where the flow came from, for example a feature id.
	Source string `json:"source,omitempty"`
	Steps  []Step `json:"steps"`
	// Soft marks a flow derived heuristically from prose. Its failures are
	// reported as advisory because the derivation may be wrong; flows the
	// agent wrote into the spec are hard.
	Soft bool `json:"soft,omitempty"`
}

// Spec is the contents of .atlas/verify.json.
type Spec struct {
	// URL is the address of the running product.
	URL string `json:"url,omitempty"`
	// Pages are extra page paths measured at every viewport.
	Pages []string `json:"pages,omitempty"`
	Flows []Flow   `json:"flows,omitempty"`
}

// LoadSpec reads and validates the spec under root. A missing file is not
// an error: it returns nil.
func LoadSpec(root string) (*Spec, error) {
	if root == "" {
		return nil, nil
	}
	path := filepath.Join(root, filepath.FromSlash(SpecFile))
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSpecBytes {
		return nil, fmt.Errorf("%s is larger than %d KiB", SpecFile, maxSpecBytes>>10)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSpec(data)
}

// ParseSpec validates a spec.
func ParseSpec(data []byte) (*Spec, error) {
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", SpecFile, err)
	}
	if spec.URL != "" {
		if _, err := CheckLocal(spec.URL); err != nil {
			return nil, fmt.Errorf("%s url: %w", SpecFile, err)
		}
	}
	if len(spec.Pages) > maxPages {
		return nil, fmt.Errorf("%s: at most %d pages", SpecFile, maxPages)
	}
	for _, page := range spec.Pages {
		if err := checkRef(page); err != nil {
			return nil, fmt.Errorf("%s pages: %w", SpecFile, err)
		}
	}
	if len(spec.Flows) > maxFlows {
		return nil, fmt.Errorf("%s: at most %d flows", SpecFile, maxFlows)
	}
	for i := range spec.Flows {
		if err := validateFlow(&spec.Flows[i]); err != nil {
			return nil, fmt.Errorf("%s flow %d: %w", SpecFile, i+1, err)
		}
	}
	return &spec, nil
}

// checkRef accepts a page path or a local address, nothing else.
func checkRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return errors.New("empty page")
	}
	if strings.Contains(ref, "://") {
		_, err := CheckLocal(ref)
		return err
	}
	if strings.HasPrefix(ref, "//") {
		return fmt.Errorf("%q is a protocol-relative address", ref)
	}
	if strings.HasPrefix(strings.ToLower(ref), "javascript:") || strings.HasPrefix(strings.ToLower(ref), "data:") {
		return fmt.Errorf("%q is not a page", ref)
	}
	return nil
}

func validateFlow(f *Flow) error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" {
		return errors.New("a flow needs a name")
	}
	if len(f.Steps) == 0 || len(f.Steps) > maxSteps {
		return fmt.Errorf("%q needs 1-%d steps", f.Name, maxSteps)
	}
	for i, s := range f.Steps {
		label := fmt.Sprintf("%q step %d", f.Name, i+1)
		if len(s.Text) > maxTextLen || len(s.Expected) > maxTextLen {
			return fmt.Errorf("%s: text is longer than %d bytes", label, maxTextLen)
		}
		hasTarget := s.Selector != "" || s.Role != "" || s.Name != "" || s.Label != ""
		switch s.Action {
		case StepNavigate:
			if err := checkRef(s.URL); err != nil {
				return fmt.Errorf("%s: %w", label, err)
			}
		case StepClick:
			if s.Selector == "" {
				return fmt.Errorf("%s: click needs a selector", label)
			}
		case StepType:
			if s.Selector == "" {
				return fmt.Errorf("%s: type needs a selector", label)
			}
		case StepKey:
			if s.Key == "" {
				return fmt.Errorf("%s: key needs a key name", label)
			}
		case StepAssert:
			if !assertConditions[s.Condition] {
				return fmt.Errorf("%s: unknown condition %q", label, s.Condition)
			}
			pageLevel := s.Condition == "url" || s.Condition == "title"
			if !pageLevel && !hasTarget {
				return fmt.Errorf("%s: %s needs a selector, role, name or label", label, s.Condition)
			}
		default:
			return fmt.Errorf("%s: unknown action %q", label, s.Action)
		}
	}
	return nil
}

// Criterion is one acceptance criterion of the work plan.
type Criterion struct {
	ID         string
	Title      string
	Acceptance string
}

var (
	quotedText = regexp.MustCompile("[\"“”`]([^\"“”`\n]{3,80})[\"“”`]")
	routeHint  = regexp.MustCompile(`(?i)(?:\b(?:route|page|screen|url|path|endpoint|sayfa|sayfas\S*|ekran|adres)\b\W{0,12}(/[A-Za-z0-9._~\-/]*)|(/[A-Za-z0-9._~\-/]+)\W{1,3}\b(?:route|page|screen|url|path|sayfa|sayfas\S*|ekran)\b)`) //nolint:misspell // Turkish keyword.
)

// DeriveFlows turns the request and the plan's acceptance criteria into
// soft flows: pages named as routes must load, and text quoted in a
// criterion must appear on the page. The derivation is deliberately small
// and its flows are advisory; a hard flow comes from the spec file.
func DeriveFlows(request string, criteria []Criterion) []Flow {
	var flows []Flow
	seenRoute := map[string]bool{}
	addRoute := func(source, text string) {
		for _, m := range routeHint.FindAllStringSubmatch(text, -1) {
			route := m[1]
			if route == "" {
				route = m[2]
			}
			if route == "" || len(route) > 100 || seenRoute[route] || looksLikeFilePath(route) {
				continue
			}
			seenRoute[route] = true
			flows = append(flows, Flow{
				Name: "page " + route + " loads", Source: source, Soft: true,
				Steps: []Step{
					{Action: StepNavigate, URL: route},
					{Action: StepAssert, Condition: "visible", Selector: "body"},
				},
			})
		}
	}
	addRoute("request", request)
	for _, c := range criteria {
		source := "feature " + c.ID
		text := strings.TrimSpace(c.Title + "\n" + c.Acceptance)
		addRoute(source, text)
		for _, m := range quotedText.FindAllStringSubmatch(c.Acceptance, -1) {
			quoted := strings.TrimSpace(m[1])
			if quoted == "" || looksLikeCode(quoted) {
				continue
			}
			flows = append(flows, Flow{
				Name: fmt.Sprintf("%s shows %q", source, quoted), Source: source, Soft: true,
				Steps: []Step{{Action: StepAssert, Condition: "text_contains", Selector: "body", Expected: quoted}},
			})
		}
	}
	if len(flows) > maxFlows {
		flows = flows[:maxFlows]
	}
	return flows
}

// looksLikeFilePath rejects routes that are really file system paths.
func looksLikeFilePath(route string) bool {
	for _, prefix := range []string{"/usr", "/etc", "/var", "/home", "/tmp", "/bin", "/opt", "/src", "/internal", "/node_modules"} {
		if route == prefix || strings.HasPrefix(route, prefix+"/") {
			return true
		}
	}
	return false
}

// looksLikeCode rejects quoted identifiers and commands, which are not text
// the user sees.
func looksLikeCode(s string) bool {
	if strings.ContainsAny(s, "(){}[]=;<>$\\") {
		return true
	}
	return !strings.ContainsRune(s, ' ') && strings.ContainsAny(s, "_./")
}

// MergeFlows returns the spec's flows followed by the derived ones whose
// name the spec did not already cover.
func MergeFlows(hard, derived []Flow) []Flow {
	out := slices.Clone(hard)
	for _, d := range derived {
		if !slices.ContainsFunc(out, func(f Flow) bool { return f.Name == d.Name }) {
			out = append(out, d)
		}
	}
	return out
}
