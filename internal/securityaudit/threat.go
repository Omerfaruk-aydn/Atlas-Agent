package securityaudit

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// maxSignalsPerKind caps each signal list so a large repository does not
// produce a prompt that crowds out the code the agent has to read.
const maxSignalsPerKind = 12

const (
	maxModelFiles    = 20000
	maxScannedBytes  = 1 << 20
	maxHotspotsShown = 15
)

type signalRule struct {
	kind    string
	entry   bool
	pattern *regexp.Regexp
}

var signalRules = []signalRule{
	{"http-handler", true, regexp.MustCompile(`\bhttp\.(HandleFunc|Handle|ListenAndServe|ListenAndServeTLS)\(|\.(GET|POST|PUT|DELETE|PATCH)\(\s*"|\bgin\.|\becho\.New|\bchi\.NewRouter|\bmux\.NewRouter`)},
	{"websocket-or-rpc", true, regexp.MustCompile(`websocket\.|\bgrpc\.NewServer|\brpc\.Register`)},
	{"cli-args", true, regexp.MustCompile(`\bos\.Args\b|\bflag\.Parse\(|cobra\.Command\{`)},
	{"environment", true, regexp.MustCompile(`\bos\.(Getenv|LookupEnv|Environ)\(`)},
	{"file-read", true, regexp.MustCompile(`\bos\.(ReadFile|Open|OpenFile)\(|\bioutil\.ReadFile\(`)},
	{"stdin", true, regexp.MustCompile(`\bos\.Stdin\b`)},
	{"deserialization", true, regexp.MustCompile(`\b(json|xml|yaml|toml)\.(Unmarshal|NewDecoder)\(|\bgob\.NewDecoder\(`)},
	{"archive-extract", true, regexp.MustCompile(`\b(zip|tar)\.(NewReader|OpenReader)\(`)},
	{"command-exec", false, regexp.MustCompile(`\bexec\.Command(Context)?\(`)},
	{"sql-query", false, regexp.MustCompile(`\b\w*([dD][bB]|[tT]x|[cC]onn|[sS]tmt|[sS]ql)\w*\.(Query|QueryRow|QueryContext|QueryRowContext|Exec|ExecContext)\(`)},
	{"file-write", false, regexp.MustCompile(`\bos\.(WriteFile|Create|MkdirAll|Mkdir|Remove|RemoveAll|Rename|Symlink)\(`)},
	{"html-template", false, regexp.MustCompile(`template\.(HTML|JS|URL)\(|"text/template"`)},
	{"outbound-request", false, regexp.MustCompile(`\bhttp\.(Get|Post|NewRequest|NewRequestWithContext)\(|\bnet\.Dial(Timeout)?\(`)},
	{"unsafe-or-reflect", false, regexp.MustCompile(`"unsafe"|\bunsafe\.Pointer`)},
	{"weak-random", false, regexp.MustCompile(`"math/rand"`)},
}

var languageByExt = map[string]string{
	".go": "go", ".py": "python", ".js": "javascript", ".mjs": "javascript", ".ts": "typescript",
	".tsx": "typescript", ".java": "java", ".kt": "kotlin", ".rs": "rust", ".c": "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cs": "csharp", ".rb": "ruby", ".php": "php", ".swift": "swift",
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"bin": true, "testdata": true, "target": true,
}

// BuildThreatModel inspects the project under root (limited to scope, a
// path under root, when non-empty) and returns the entry points, sinks and
// trust boundaries it can see without running anything. The result is a
// map for the agent, not a verdict: a sink is not a vulnerability.
func BuildThreatModel(ctx context.Context, root, scope string) (ThreatModel, error) {
	start := root
	if scope != "" {
		start = scope
		if !filepath.IsAbs(start) {
			start = filepath.Join(root, start)
		}
	}
	model := ThreatModel{Languages: map[string]int{}}
	perKind := map[string]int{}
	perFile := map[string]int{}

	err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != start && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		lang, ok := languageByExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		if model.Files >= maxModelFiles {
			model.Truncated = true
			return filepath.SkipAll
		}
		model.Files++
		model.Languages[lang]++
		if lang != "go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanSignals(&model, perKind, perFile, root, path)
		return nil
	})
	if err != nil {
		return model, err
	}

	for file := range perFile {
		model.Hotspots = append(model.Hotspots, file)
	}
	sort.Slice(model.Hotspots, func(i, j int) bool {
		a, b := model.Hotspots[i], model.Hotspots[j]
		if perFile[a] != perFile[b] {
			return perFile[a] > perFile[b]
		}
		return a < b
	})
	if len(model.Hotspots) > maxHotspotsShown {
		model.Hotspots = model.Hotspots[:maxHotspotsShown]
	}
	deriveBoundaries(&model, perKind)
	return model, nil
}

func scanSignals(model *ThreatModel, perKind, perFile map[string]int, root, path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxScannedBytes {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannedBytes)
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, rule := range signalRules {
			if !rule.pattern.MatchString(text) {
				continue
			}
			perKind[rule.kind]++
			perFile[rel]++
			if perKind[rule.kind] > maxSignalsPerKind {
				model.Truncated = true
				continue
			}
			sig := Signal{Kind: rule.kind, File: rel, Line: line, Detail: clip(trimmed, 120)}
			if rule.entry {
				model.EntryPoints = append(model.EntryPoints, sig)
			} else {
				model.Sinks = append(model.Sinks, sig)
			}
		}
	}
}

func deriveBoundaries(model *ThreatModel, perKind map[string]int) {
	add := func(kind, text string) {
		if perKind[kind] > 0 {
			model.Boundaries = append(model.Boundaries, text)
		}
	}
	add("http-handler", "Network clients to HTTP handlers: every request header, path, query and body is attacker-controlled.")
	add("websocket-or-rpc", "Remote peers to RPC or websocket endpoints: message contents and origin are attacker-controlled.")
	add("cli-args", "Command-line callers to argument parsing: arguments may come from scripts that relay untrusted text.")
	add("environment", "Process environment to configuration: usually operator-controlled, attacker-controlled in shared CI or wrapper scripts.")
	add("file-read", "Files on disk to the program: contents and names may be written by another user or an earlier, less trusted step.")
	add("deserialization", "Serialized input to decoders: shape, size and nesting are attacker-controlled when the source is.")
	add("archive-extract", "Archives to the filesystem: entry names and link targets are attacker-controlled.")
	add("outbound-request", "Program to remote hosts: a URL built from input can reach internal addresses.")
	add("command-exec", "Program to operating-system commands: any attacker data in the command line is a risk.")
	add("sql-query", "Program to database: queries built from strings carry input into the query language.")

	model.Assumptions = append(model.Assumptions,
		"Signals are pattern matches on Go source, not data-flow results: a sink with no attacker path is not a vulnerability.",
		"Only Go proof tests can be verified by this tool; other languages are listed but their findings stay unconfirmed.")
	if model.Languages["go"] == 0 {
		model.Assumptions = append(model.Assumptions, "No Go source was found in scope, so no finding can be confirmed.")
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
