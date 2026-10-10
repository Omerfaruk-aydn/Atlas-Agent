package secrets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one suspected credential.
type Finding struct {
	Rule       string
	Confidence Confidence
	File       string
	Line       int
	// Redacted is the matched value with its middle replaced, so a report
	// can be read, pasted and logged without re-leaking the secret it is
	// warning about.
	Redacted string
	// Context is the source line, itself redacted.
	Context string
}

// Result is the outcome of a scan.
type Result struct {
	Findings     []Finding
	FilesScanned int
	FilesSkipped int
	// Truncated reports that scanning stopped early at the finding limit.
	Truncated bool
}

// Options tune a scan.
type Options struct {
	// MaxFileSize skips files larger than this many bytes. A large file
	// is nearly always a binary, a lockfile or a fixture, and scanning it
	// costs more than it finds. Zero means the default.
	MaxFileSize int64
	// MaxFindings stops the scan once this many findings exist. Zero
	// means the default.
	MaxFindings int
	// MinConfidence drops findings below this level.
	MinConfidence Confidence
	// IncludeGeneric enables the keyword+entropy layer. On by default via
	// the zero value being false-means-include; see Scan.
	SkipGeneric bool
}

const (
	defaultMaxFileSize = 1 << 20 // 1 MiB
	defaultMaxFindings = 200
	// maxLineLength skips absurdly long lines. A minified bundle or a
	// base64 blob on one line produces nothing but noise, and running
	// every rule over a megabyte-long line is slow.
	maxLineLength = 4096
)

// skipDirs are trees that contain other people's code or build output.
// A key found in node_modules is not this repository's leak, and
// reporting it buries the ones that are.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true, ".next": true,
	"coverage": true, ".terraform": true,
}

// skipExts are file types whose contents cannot usefully be scanned as
// text, or that are generated.
var skipExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
	".pdf": true, ".zip": true, ".gz": true, ".tar": true, ".bz2": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".mp4": true,
	".mp3": true, ".wav": true, ".class": true, ".jar": true, ".wasm": true,
	".sum": true, ".lock": true,
}

// Scan walks root and reports suspected credentials.
//
// It reads files as text line by line, so memory stays flat regardless of
// tree size, and it never returns a raw secret: every value is redacted
// before it leaves this package. A report that quoted the key in full
// would put the credential into a transcript, a log and quite possibly a
// bug tracker -- turning a warning into a second leak.
func Scan(root string, opts Options) (Result, error) {
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = defaultMaxFileSize
	}
	if opts.MaxFindings <= 0 {
		opts.MaxFindings = defaultMaxFindings
	}

	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("cannot scan %s: %w", root, err)
	}

	var result Result
	scanOne := func(path string, size int64) {
		if size > opts.MaxFileSize {
			result.FilesSkipped++
			return
		}
		findings, err := scanFile(path, opts)
		if err != nil {
			result.FilesSkipped++
			return
		}
		result.FilesScanned++
		result.Findings = append(result.Findings, findings...)
	}

	if !info.IsDir() {
		scanOne(root, info.Size())
	} else {
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if skipExts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			if len(result.Findings) >= opts.MaxFindings {
				result.Truncated = true
				return filepath.SkipAll
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			scanOne(path, fi.Size())
			return nil
		})
		if err != nil {
			return result, err
		}
	}

	if len(result.Findings) > opts.MaxFindings {
		result.Findings = result.Findings[:opts.MaxFindings]
		result.Truncated = true
	}

	// Highest confidence first: the reader's attention is the scarce
	// resource, and a high-confidence finding is the one that matters.
	rank := map[Confidence]int{ConfidenceHigh: 0, ConfidenceMedium: 1, ConfidenceLow: 2}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if rank[result.Findings[i].Confidence] != rank[result.Findings[j].Confidence] {
			return rank[result.Findings[i].Confidence] < rank[result.Findings[j].Confidence]
		}
		if result.Findings[i].File != result.Findings[j].File {
			return result.Findings[i].File < result.Findings[j].File
		}
		return result.Findings[i].Line < result.Findings[j].Line
	})

	return result, nil
}

func scanFile(path string, opts Options) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineLength)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if len(line) > maxLineLength || !isMostlyText(line) {
			continue
		}
		findings = append(findings, scanLine(path, lineNo, line, opts)...)
	}
	// A scanner error (a long line, a binary file) ends the file early
	// rather than failing the scan: whatever was read is still valid.
	return findings, nil
}

// ScanText applies the same line rules to in-memory text, so a document
// can be checked before it is ever written to disk. name labels findings.
func ScanText(name, text string, opts Options) []Finding {
	var findings []Finding
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if len(line) > maxLineLength || !isMostlyText(line) {
			continue
		}
		findings = append(findings, scanLine(name, i+1, line, opts)...)
	}
	return findings
}

func scanLine(path string, lineNo int, line string, opts Options) []Finding {
	// A line that disables the check is honoured, so a documented test
	// fixture or a rotated key does not have to be reported forever.
	if strings.Contains(line, "atlas:allow-secret") || strings.Contains(line, "gitleaks:allow") {
		return nil
	}

	var out []Finding
	seen := map[string]bool{}

	rules := namedRules
	if !opts.SkipGeneric {
		rules = append(append([]Rule(nil), namedRules...), genericRule)
	}

	for _, rule := range rules {
		for _, m := range rule.Pattern.FindAllStringSubmatch(line, -1) {
			value := m[0]
			if rule.Group > 0 && rule.Group < len(m) {
				value = m[rule.Group]
			}
			if value == "" || isPlaceholder(value) {
				continue
			}

			confidence := rule.Confidence
			if rule.MinEntropy > 0 {
				entropy := shannonEntropy(value)
				if entropy < rule.MinEntropy {
					continue
				}
				// A high-entropy value under a secret-sounding name is
				// worth more attention than a merely random-ish one.
				if entropy >= 4.0 {
					confidence = ConfidenceMedium
				} else {
					confidence = ConfidenceLow
				}
			}
			if !meetsConfidence(confidence, opts.MinConfidence) {
				continue
			}

			key := rule.Name + "\x00" + value
			if seen[key] {
				continue
			}
			seen[key] = true

			out = append(out, Finding{
				Rule:       rule.Name,
				Confidence: confidence,
				File:       path,
				Line:       lineNo,
				Redacted:   Redact(value),
				Context:    strings.TrimSpace(redactIn(line, value)),
			})
		}
	}
	return out
}

func meetsConfidence(got, floor Confidence) bool {
	rank := map[Confidence]int{ConfidenceLow: 0, ConfidenceMedium: 1, ConfidenceHigh: 2}
	if floor == "" {
		return true
	}
	return rank[got] >= rank[floor]
}

// Redact keeps enough of a value to recognise which credential it is --
// so it can be found and rotated -- while never reproducing it.
func Redact(value string) string {
	const keep = 4
	if len(value) <= keep*2 {
		return strings.Repeat("*", len(value))
	}
	return value[:keep] + strings.Repeat("*", 8) + value[len(value)-keep:]
}

// redactIn replaces a secret inside its surrounding line.
func redactIn(line, value string) string {
	if value == "" {
		return line
	}
	return strings.ReplaceAll(line, value, Redact(value))
}

// isMostlyText rejects a line that is largely non-printable, which is how
// a binary file that slipped past the extension filter announces itself.
func isMostlyText(line string) bool {
	if line == "" {
		return true
	}
	nonPrintable := 0
	for i := range len(line) {
		c := line[i]
		if c < 0x09 || (c > 0x0d && c < 0x20) || c == 0x7f {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(len(line)) < 0.1
}
