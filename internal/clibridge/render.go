package clibridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseAssignments turns caller "name=value" entries into values per name.
// Only list params may repeat. Every name must be declared by the operation.
func ParseAssignments(op Operation, entries []string) (map[string][]string, error) {
	if len(entries) > maxParams*maxListItems {
		return nil, fmt.Errorf("too many arguments")
	}
	out := map[string][]string{}
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("argument %q must use name=value", truncate(entry, 64))
		}
		p, declared := op.Param(name)
		if !declared {
			names := make([]string, 0, len(op.Params))
			for _, p := range op.Params {
				names = append(names, p.Name)
			}
			return nil, fmt.Errorf("unknown parameter %q for operation %s; declared: %s", truncate(name, 64), op.ID, strings.Join(names, ", "))
		}
		if _, repeated := out[name]; repeated && p.Type != TypeList {
			return nil, fmt.Errorf("parameter %q given more than once; only list params repeat", name)
		}
		out[name] = append(out[name], value)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// typedValue is one validated caller value before rendering.
type typedValue struct {
	text    string
	boolean bool
}

// parseValues validates every raw value against the param contract.
func (p Param) parseValues(raw []string) ([]typedValue, error) {
	itemType := p.Type
	if p.Type == TypeList {
		itemType = p.ItemType
		if len(raw) < p.MinItems || len(raw) > p.MaxItems {
			return nil, fmt.Errorf("%s requires %d-%d items, got %d", p.Name, p.MinItems, p.MaxItems, len(raw))
		}
	} else if len(raw) != 1 {
		return nil, fmt.Errorf("%s requires exactly one value", p.Name)
	}
	out := make([]typedValue, 0, len(raw))
	for _, value := range raw {
		if len(value) > maxArgBytes || strings.ContainsRune(value, 0) || !utf8.ValidString(value) {
			return nil, fmt.Errorf("%s must be valid UTF-8 up to %d bytes without NUL", p.Name, maxArgBytes)
		}
		v, err := p.parseScalar(itemType, value)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (p Param) parseScalar(itemType, value string) (typedValue, error) {
	switch itemType {
	case TypeString:
		limit := p.MaxLength
		if limit == 0 {
			limit = defaultMaxLength
		}
		length := utf8.RuneCountInString(value)
		if length < p.MinLength || length > limit {
			return typedValue{}, fmt.Errorf("%s length must be %d-%d characters", p.Name, p.MinLength, limit)
		}
		if strings.HasPrefix(value, "-") && !p.AllowLeadingDash {
			return typedValue{}, fmt.Errorf("%s may not start with '-' (it could be read as an option); the profile must set allow_leading_dash to permit it", p.Name)
		}
		if p.Pattern != "" {
			// Patterns are anchored so they describe the whole value.
			re, err := regexp.Compile(`^(?:` + p.Pattern + `)$`)
			if err != nil || !re.MatchString(value) {
				return typedValue{}, fmt.Errorf("%s does not match the profile pattern", p.Name)
			}
		}
		return typedValue{text: value}, nil
	case TypeInteger:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return typedValue{}, fmt.Errorf("%s must be an integer, got %q", p.Name, truncate(value, 32))
		}
		if float64(n) < *p.Min || float64(n) > *p.Max {
			return typedValue{}, fmt.Errorf("%s must be between %s and %s", p.Name, formatNumber(*p.Min), formatNumber(*p.Max))
		}
		return typedValue{text: strconv.FormatInt(n, 10)}, nil
	case TypeNumber:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return typedValue{}, fmt.Errorf("%s must be a finite number, got %q", p.Name, truncate(value, 32))
		}
		if n < *p.Min || n > *p.Max {
			return typedValue{}, fmt.Errorf("%s must be between %s and %s", p.Name, formatNumber(*p.Min), formatNumber(*p.Max))
		}
		return typedValue{text: formatNumber(n)}, nil
	case TypeBoolean:
		switch value {
		case "true":
			return typedValue{text: value, boolean: true}, nil
		case "false":
			return typedValue{text: value}, nil
		}
		return typedValue{}, fmt.Errorf("%s must be true or false", p.Name)
	case TypeEnum:
		if !slices.Contains(p.Values, value) {
			return typedValue{}, fmt.Errorf("%s must be one of %s", p.Name, strings.Join(p.Values, ", "))
		}
		return typedValue{text: value}, nil
	case TypeInputFile, TypeOutputFile:
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return typedValue{}, fmt.Errorf("%s requires a file path", p.Name)
		}
		if len(p.Extensions) > 0 && !slices.ContainsFunc(p.Extensions, func(ext string) bool { return strings.EqualFold(filepath.Ext(value), ext) }) {
			return typedValue{}, fmt.Errorf("%s must have one of the extensions %s", p.Name, strings.Join(p.Extensions, ", "))
		}
		return typedValue{text: value}, nil
	}
	return typedValue{}, fmt.Errorf("unsupported type %s", itemType)
}

func formatNumber(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

// FileRef is a classified file argument.
type FileRef struct {
	Param     string `json:"param"`
	Path      string `json:"path"`
	Relative  string `json:"relative,omitempty"`
	External  bool   `json:"external,omitempty"`
	Existed   bool   `json:"existed,omitempty"`
	Size      int64  `json:"size,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ModTimeNS int64  `json:"mod_time_ns,omitempty"`
}

// EnvStatus reports whether a declared variable is present. Values are
// never read into reports.
type EnvStatus struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

// Prepared is a fully validated invocation bound to one profile revision,
// one executable identity and one literal argv.
type Prepared struct {
	ID                string              `json:"prepare_id"`
	ProfileID         string              `json:"profile_id"`
	Scope             Scope               `json:"scope,omitempty"`
	ProfileHash       string              `json:"profile_hash"`
	Operation         string              `json:"operation"`
	Executable        Identity            `json:"executable"`
	Argv              []string            `json:"-"`
	DisplayArgv       []string            `json:"argv"`
	WorkingDir        string              `json:"working_dir"`
	Inputs            []FileRef           `json:"inputs,omitempty"`
	Outputs           []FileRef           `json:"outputs,omitempty"`
	Env               []EnvStatus         `json:"env,omitempty"`
	TimeoutSeconds    int                 `json:"timeout_seconds"`
	MaxOutputBytes    int                 `json:"max_output_bytes"`
	ExpectedExitCodes []int               `json:"expected_exit_codes"`
	Expectations      []OutputExpectation `json:"output_expectations,omitempty"`
	StdoutContains    string              `json:"stdout_contains,omitempty"`
	BackgroundAllowed bool                `json:"background_allowed,omitempty"`
	Secrets           []string            `json:"-"`
	Effects           []string            `json:"effects"`
}

// RenderOptions bind rendering to the project.
type RenderOptions struct {
	// Root is the absolute project root. File params and the working
	// directory are confined to it unless an input allows external files.
	Root string
	// LookupEnv reports declared variables; nil uses os.LookupEnv.
	LookupEnv func(string) (string, bool)
}

// Prepare validates caller values and renders the literal argv. It performs
// no execution and does not write files.
func Prepare(installed Installed, identity Identity, opID string, entries []string, opts RenderOptions) (Prepared, error) {
	p := installed.Profile
	op, err := p.Operation(opID)
	if err != nil {
		return Prepared{}, err
	}
	if !filepath.IsAbs(opts.Root) {
		return Prepared{}, fmt.Errorf("project root must be absolute")
	}
	assignments, err := ParseAssignments(op, entries)
	if err != nil {
		return Prepared{}, err
	}
	timeout, output, exits := op.Limits()
	prepared := Prepared{
		ProfileID: p.ID, Scope: installed.Scope, ProfileHash: installed.ProfileSHA256, Operation: op.ID, Executable: identity,
		TimeoutSeconds: timeout, MaxOutputBytes: output, ExpectedExitCodes: exits, Expectations: op.Outputs,
		StdoutContains: op.StdoutContains, BackgroundAllowed: op.Background,
	}
	wd, err := projectPath(opts.Root, cmpOr(op.WorkingDir, "."))
	if err != nil {
		return Prepared{}, fmt.Errorf("working_dir: %w", err)
	}
	if info, err := os.Stat(wd); err != nil || !info.IsDir() {
		return Prepared{}, fmt.Errorf("working_dir %q is not an existing project directory", cmpOr(op.WorkingDir, "."))
	}
	prepared.WorkingDir = wd
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	for _, name := range p.Env {
		_, present := lookup(name)
		prepared.Env = append(prepared.Env, EnvStatus{Name: name, Present: present})
		if !present {
			return Prepared{}, fmt.Errorf("required environment variable %s is not set; set it in the environment Atlas runs in (its value is never stored or shown)", name)
		}
	}
	argv := []string{identity.Path}
	display := []string{identity.Path}
	params := map[string]Param{}
	for _, param := range op.Params {
		params[param.Name] = param
	}
	for _, arg := range op.Args {
		tpl, _ := parseTemplate(arg)
		if tpl.param == "" {
			argv = append(argv, tpl.prefix)
			display = append(display, tpl.prefix)
			continue
		}
		param := params[tpl.param]
		raw, given := assignments[param.Name]
		if !given && len(param.Default) > 0 {
			if err := decodeDefault(param, &raw); err != nil {
				return Prepared{}, err
			}
			given = true
		}
		if !given {
			if param.Required {
				return Prepared{}, fmt.Errorf("missing required parameter %s", param.Name)
			}
			continue
		}
		values, err := param.parseValues(raw)
		if err != nil {
			return Prepared{}, err
		}
		for _, value := range values {
			rendered := value.text
			shown := value.text
			itemType := param.Type
			if itemType == TypeList {
				itemType = param.ItemType
			}
			switch itemType {
			case TypeBoolean:
				if !value.boolean {
					continue
				}
				argv = append(argv, param.Flag)
				display = append(display, param.Flag)
				continue
			case TypeInputFile:
				ref, err := resolveInput(opts.Root, param, value.text)
				if err != nil {
					return Prepared{}, err
				}
				prepared.Inputs = append(prepared.Inputs, ref)
				rendered, shown = ref.Path, ref.Path
			case TypeOutputFile:
				ref, err := resolveOutput(opts.Root, param, value.text)
				if err != nil {
					return Prepared{}, err
				}
				prepared.Outputs = append(prepared.Outputs, ref)
				rendered, shown = ref.Path, ref.Path
			}
			if param.Secret {
				prepared.Secrets = append(prepared.Secrets, value.text)
				shown = "[REDACTED]"
			}
			if param.Flag != "" {
				argv = append(argv, param.Flag)
				display = append(display, param.Flag)
			}
			argv = append(argv, tpl.prefix+rendered+tpl.suffix)
			display = append(display, tpl.prefix+shown+tpl.suffix)
		}
	}
	if len(argv) > 128 {
		return Prepared{}, fmt.Errorf("rendered argv exceeds 128 elements")
	}
	total := 0
	for _, arg := range argv {
		total += len(arg)
	}
	if total > 16*1024 {
		return Prepared{}, fmt.Errorf("rendered argv exceeds 16 KiB")
	}
	prepared.Argv, prepared.DisplayArgv = argv, display
	prepared.Effects = effects(prepared)
	prepared.ID = prepareID(prepared)
	return prepared, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// prepareID binds the profile revision, operation, executable content and
// full argv. Secret values contribute only through a digest of the argv.
func prepareID(p Prepared) string {
	argv, _ := json.Marshal(p.Argv)
	argvSum := sha256.Sum256(argv)
	data, _ := json.Marshal([]string{p.ProfileID, string(p.Scope), p.ProfileHash, p.Operation, p.Executable.Path, p.Executable.SHA256, hex.EncodeToString(argvSum[:]), p.WorkingDir})
	sum := sha256.Sum256(data)
	return "prep-" + hex.EncodeToString(sum[:12])
}

func effects(p Prepared) []string {
	out := []string{"Runs " + p.Executable.Path + " (" + string(p.Executable.Kind) + ", sha256 " + shortHash(p.Executable.SHA256) + ") with the literal argv shown; no shell parses it."}
	out = append(out, "Working directory: "+p.WorkingDir+"; stops after "+strconv.Itoa(p.TimeoutSeconds)+"s.")
	for _, in := range p.Inputs {
		note := "Reads " + in.Path
		if in.External {
			note += " (outside the project; requires explicit read permission)"
		}
		out = append(out, note)
	}
	for _, o := range p.Outputs {
		verb := "Creates "
		if o.Existed {
			verb = "Overwrites "
		}
		out = append(out, verb+o.Path)
	}
	out = append(out, "The profile does not limit other side effects the program itself performs; the profile's description is a claim, not a sandbox.")
	return out
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// projectPath confines a relative path to root and refuses symlinked
// components so a later swap cannot redirect the path outside the project.
func projectPath(root, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." {
		return root, nil
	}
	if !filepath.IsLocal(clean) || strings.ContainsAny(relative, "\x00\r\n") {
		return "", fmt.Errorf("path %q escapes the project", relative)
	}
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinked project path is unsupported: %s", relative)
		}
	}
	return current, nil
}

// relativeToRoot returns a project-relative path when value names a file
// inside root, whether value is relative or absolute.
func relativeToRoot(root, value string) (string, bool) {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(value))
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}

func resolveInput(root string, p Param, value string) (FileRef, error) {
	ref := FileRef{Param: p.Name}
	if rel, inside := relativeToRoot(root, value); inside {
		path, err := projectPath(root, rel)
		if err != nil {
			return FileRef{}, fmt.Errorf("%s: %w", p.Name, err)
		}
		ref.Path, ref.Relative = path, filepath.ToSlash(rel)
	} else {
		if !filepath.IsAbs(value) {
			return FileRef{}, fmt.Errorf("%s: %q escapes the project", p.Name, value)
		}
		if !p.AllowExternal {
			return FileRef{}, fmt.Errorf("%s: %q is outside the project and the profile does not allow external input files", p.Name, value)
		}
		resolved, err := filepath.EvalSymlinks(value)
		if err != nil {
			return FileRef{}, fmt.Errorf("%s: %w", p.Name, err)
		}
		ref.Path, ref.External = resolved, true
	}
	info, err := os.Stat(ref.Path)
	if err != nil {
		return FileRef{}, fmt.Errorf("%s: input file %q does not exist", p.Name, value)
	}
	if !info.Mode().IsRegular() {
		return FileRef{}, fmt.Errorf("%s: input %q is not a regular file", p.Name, value)
	}
	ref.Size = info.Size()
	return ref, nil
}

func resolveOutput(root string, p Param, value string) (FileRef, error) {
	rel, inside := relativeToRoot(root, value)
	if !inside {
		return FileRef{}, fmt.Errorf("%s: output %q must be inside the project", p.Name, value)
	}
	path, err := projectPath(root, rel)
	if err != nil {
		return FileRef{}, fmt.Errorf("%s: %w", p.Name, err)
	}
	ref := FileRef{Param: p.Name, Path: path, Relative: filepath.ToSlash(rel)}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return FileRef{}, fmt.Errorf("%s: the directory for %q does not exist", p.Name, value)
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return FileRef{}, fmt.Errorf("%s: %w", p.Name, err)
	case !info.Mode().IsRegular():
		return FileRef{}, fmt.Errorf("%s: %q exists and is not a regular file", p.Name, value)
	case !p.Overwrite:
		return FileRef{}, fmt.Errorf("%s: %q already exists and the profile does not allow overwriting it", p.Name, value)
	default:
		ref.Existed = true
		ref.Size = info.Size()
		ref.ModTimeNS = info.ModTime().UnixNano()
		if sum, err := fileSHA256(path, maxHashBytes); err == nil {
			ref.SHA256 = sum
		}
	}
	return ref, nil
}
