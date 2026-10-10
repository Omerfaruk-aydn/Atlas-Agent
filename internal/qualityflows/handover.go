package qualityflows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type HandoverStep struct {
	Name           string   `json:"name"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	ExpectedExit   int      `json:"expected_exit,omitempty"`
	OutputContains string   `json:"output_contains,omitempty"`
	File           string   `json:"file,omitempty"`
	FileContains   string   `json:"file_contains,omitempty"`
	ReadmeQuote    string   `json:"readme_quote"`
}

type HandoverSpec struct {
	Version int            `json:"version"`
	Readme  string         `json:"readme"`
	Steps   []HandoverStep `json:"steps"`
}

type Observation struct {
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
}

type HandoverResult struct {
	Name        string       `json:"name"`
	Status      string       `json:"status"`
	Observation *Observation `json:"observation,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

type HandoverReport struct {
	Passed       bool             `json:"passed"`
	SpecHash     string           `json:"spec_hash"`
	ReadmeHash   string           `json:"readme_hash"`
	SnapshotHash string           `json:"snapshot_hash"`
	Results      []HandoverResult `json:"results"`
	Excluded     []string         `json:"excluded"`
	Environment  string           `json:"environment"`
}

type CommandRunner func(context.Context, string, []string) (Observation, error)

func ValidateHandover(spec HandoverSpec, readme []byte) error {
	if spec.Version != 1 || len(spec.Steps) == 0 || len(spec.Steps) > 24 {
		return fmt.Errorf("version 1 and 1-24 steps required")
	}
	names := map[string]bool{}
	for _, step := range spec.Steps {
		if step.Name == "" || len(step.Name) > 128 || names[step.Name] || len(step.Argv) == 0 || len(step.Argv) > 64 || step.Argv[0] == "" || step.TimeoutSeconds < 0 || step.TimeoutSeconds > 600 || step.ReadmeQuote == "" || len(step.ReadmeQuote) > 4096 || !strings.Contains(string(readme), step.ReadmeQuote) || len(step.OutputContains) > 4096 || len(step.FileContains) > 4096 {
			return fmt.Errorf("invalid step or missing literal README evidence: %s", step.Name)
		}
		names[step.Name] = true
		for _, arg := range step.Argv {
			if len(arg) > 32768 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("invalid command argument")
			}
		}
		if step.File == "" && step.FileContains != "" {
			return fmt.Errorf("file_contains requires file")
		}
		if step.File != "" {
			if _, err := ProjectPath(".", step.File); err != nil {
				return err
			}
		}
	}
	return nil
}

func excludedPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		part = strings.ToLower(part)
		if part == ".git" || part == ".atlas" || part == "node_modules" || part == ".venv" || part == "vendor" || part == "dist" || part == "build" || part == ".next" || (strings.HasPrefix(part, ".env") && part != ".env.example" && part != ".env.sample" && part != ".env.template") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || part == "credentials.json" || part == "atlasrc" || part == "atlas.json" || part == ".npmrc" || part == ".pypirc" || part == ".netrc" {
			return true
		}
	}
	return false
}

// RunHandover copies bounded regular Git-visible files into a disposable
// directory. This tests a clean project tree on this host, not a clean OS.
func RunHandover(ctx context.Context, root string, spec HandoverSpec, run CommandRunner) (HandoverReport, error) {
	report := HandoverReport{Passed: true, Results: []HandoverResult{}, Excluded: []string{}, Environment: "Disposable project copy on the current host; installed runtimes and runner environment remain available. Not a sandbox."}
	readme, err := ReadFile(root, spec.Readme, 256*1024)
	if err != nil {
		return report, err
	}
	if err := ValidateHandover(spec, readme); err != nil {
		return report, err
	}
	if run == nil {
		return report, fmt.Errorf("command runner unavailable")
	}
	report.ReadmeHash = engineering.Hash(string(readme))
	specData, _ := json.Marshal(spec)
	report.SpecHash = engineering.Hash(string(specData))
	files, err := git(ctx, root, nil, nil, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return report, err
	}
	dir, err := os.MkdirTemp("", "atlas-handover-")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(dir)
	seen := map[string]bool{}
	identities := map[string]string{}
	var total int64
	for _, path := range strings.Split(string(files), "\x00") {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if len(seen) > 8192 {
			return report, fmt.Errorf("project copy exceeds 8192 files")
		}
		if excludedPath(path) {
			report.Excluded = append(report.Excluded, path)
			continue
		}
		data, err := ReadFile(root, path, maxBytes)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return report, fmt.Errorf("copy %s: %w", path, err)
		}
		total += int64(len(data))
		if total > 128*1024*1024 {
			return report, fmt.Errorf("project copy exceeds 128 MiB")
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return report, err
		}
		original, _ := ProjectPath(root, path)
		info, err := os.Stat(original)
		if err != nil {
			return report, err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return report, err
		}
		identities[path] = engineering.Hash(string(data))
	}
	if identities[filepath.ToSlash(filepath.Clean(spec.Readme))] != report.ReadmeHash {
		return report, fmt.Errorf("README is excluded, ignored or changed during copy")
	}
	encoded, _ := json.Marshal(identities)
	report.SnapshotHash = engineering.Hash(string(encoded))
	for _, step := range spec.Steps {
		if !report.Passed {
			report.Results = append(report.Results, HandoverResult{Name: step.Name, Status: "blocked", Reason: "previous step failed; not executed"})
			continue
		}
		timeout := step.TimeoutSeconds
		if timeout == 0 {
			timeout = 120
		}
		stepCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		observed, err := run(stepCtx, dir, step.Argv)
		cancel()
		result := HandoverResult{Name: step.Name, Status: "passed", Observation: &observed}
		if err != nil {
			result.Status = "failed"
			result.Reason = err.Error()
			result.Observation = nil
		} else if observed.ExitCode != step.ExpectedExit || !strings.Contains(observed.Output, step.OutputContains) {
			result.Status = "failed"
			result.Reason = "observed exit/output did not meet expectations"
		}
		if result.Status == "passed" && step.File != "" {
			data, err := ReadFile(dir, step.File, maxBytes)
			if err != nil || !strings.Contains(string(data), step.FileContains) {
				result.Status = "failed"
				result.Reason = "expected regular file/content was not observed"
			}
		}
		if len(observed.Output) > 8192 && result.Observation != nil {
			result.Observation.Output = observed.Output[:8192] + "\n[truncated]"
		}
		report.Results = append(report.Results, result)
		report.Passed = report.Passed && result.Status == "passed"
	}
	return report, nil
}
