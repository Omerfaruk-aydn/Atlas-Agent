package qualityflows

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type BoundaryTarget struct {
	Path   string   `json:"path"`
	Expect string   `json:"expect"`
	Labels []string `json:"labels,omitempty"`
}

type BoundarySpec struct {
	Version int              `json:"version"`
	Action  string           `json:"action"`
	ProbeID string           `json:"probe_id,omitempty"`
	Labels  []string         `json:"labels,omitempty"`
	Targets []BoundaryTarget `json:"targets,omitempty"`
}

type BoundaryProbe struct {
	ID      string            `json:"id"`
	Root    string            `json:"root"`
	Markers map[string]string `json:"markers"`
}

type BoundaryMatch struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	Encoding string `json:"encoding"`
	Offset   int    `json:"offset"`
	Line     int    `json:"line"`
}

type BoundaryResult struct {
	Path   string         `json:"path"`
	Expect string         `json:"expect"`
	Passed bool           `json:"passed"`
	Files  int            `json:"files"`
	Counts map[string]int `json:"counts"`
	Reason string         `json:"reason,omitempty"`
}

type BoundaryReport struct {
	Passed      bool              `json:"passed"`
	ProbeID     string            `json:"probe_id"`
	SpecHash    string            `json:"spec_hash"`
	InputHashes map[string]string `json:"input_hashes"`
	Results     []BoundaryResult  `json:"results"`
	Matches     []BoundaryMatch   `json:"matches"`
}

func ValidateBoundary(spec BoundarySpec) error {
	if spec.Version != 1 {
		return fmt.Errorf("version 1 required")
	}
	validLabels := func(labels []string) bool {
		seen := map[string]bool{}
		if len(labels) > 32 {
			return false
		}
		for _, label := range labels {
			if !datasetName.MatchString(label) || seen[label] {
				return false
			}
			seen[label] = true
		}
		return true
	}
	switch spec.Action {
	case "prepare":
		if len(spec.Labels) == 0 || !validLabels(spec.Labels) || spec.ProbeID != "" || len(spec.Targets) > 0 {
			return fmt.Errorf("prepare requires 1-32 unique labels only")
		}
	case "scan":
		if len(spec.ProbeID) != 32 || len(spec.Targets) == 0 || len(spec.Targets) > 32 || len(spec.Labels) > 0 {
			return fmt.Errorf("scan requires probe_id and 1-32 targets")
		}
		if _, err := hex.DecodeString(spec.ProbeID); err != nil {
			return fmt.Errorf("invalid probe ID")
		}
		for _, target := range spec.Targets {
			if _, err := ProjectPath(".", target.Path); err != nil {
				return err
			}
			if target.Expect != "absent" && target.Expect != "present" || !validLabels(target.Labels) {
				return fmt.Errorf("target requires present/absent and valid labels")
			}
		}
	default:
		return fmt.Errorf("boundary action must be prepare or scan")
	}
	return nil
}

func PrepareBoundary(root string, spec BoundarySpec) (BoundaryProbe, error) {
	if err := ValidateBoundary(spec); err != nil {
		return BoundaryProbe{}, err
	}
	if spec.Action != "prepare" {
		return BoundaryProbe{}, fmt.Errorf("prepare action required")
	}
	identity := make([]byte, 16)
	if _, err := rand.Read(identity); err != nil {
		return BoundaryProbe{}, err
	}
	probe := BoundaryProbe{ID: hex.EncodeToString(identity), Root: root, Markers: map[string]string{}}
	for _, label := range spec.Labels {
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			return BoundaryProbe{}, err
		}
		probe.Markers[label] = "ATLAS_SYNTHETIC_" + label + "_" + hex.EncodeToString(token)
	}
	return probe, nil
}

// ScanBoundary reads explicit project paths and emits locations, never snippets.
// Absence is meaningful only when every selected input was readable and bounded.
func ScanBoundary(ctx context.Context, root string, spec BoundarySpec, probe BoundaryProbe) (BoundaryReport, error) {
	report := BoundaryReport{ProbeID: probe.ID, InputHashes: map[string]string{}, Results: []BoundaryResult{}, Matches: []BoundaryMatch{}}
	if err := ValidateBoundary(spec); err != nil {
		return report, err
	}
	if spec.Action != "scan" || spec.ProbeID != probe.ID || !SameRoot(root, probe.Root) || len(probe.Markers) == 0 || len(probe.Markers) > 32 {
		return report, fmt.Errorf("probe does not belong to this project")
	}
	for label, marker := range probe.Markers {
		if !datasetName.MatchString(label) || !strings.HasPrefix(marker, "ATLAS_SYNTHETIC_"+label+"_") || len(marker) != len("ATLAS_SYNTHETIC_"+label+"_")+32 {
			return report, fmt.Errorf("invalid synthetic probe")
		}
	}
	for _, target := range spec.Targets {
		for _, label := range target.Labels {
			if probe.Markers[label] == "" {
				return report, fmt.Errorf("target references unknown probe label")
			}
		}
	}
	encoded, _ := json.Marshal(spec)
	report.SpecHash = engineering.Hash(string(encoded))
	report.Passed = true
	var total int64
	files := 0
	for _, target := range spec.Targets {
		result := BoundaryResult{Path: target.Path, Expect: target.Expect, Passed: true, Counts: map[string]int{}}
		labels := target.Labels
		if len(labels) == 0 {
			for label := range probe.Markers {
				labels = append(labels, label)
			}
		}
		labels = append([]string{}, labels...)
		slices.Sort(labels)
		for _, label := range labels {
			result.Counts[label] = 0
		}
		path, err := ProjectPath(root, target.Path)
		if err == nil {
			err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("symlinked scan input refused")
				}
				if entry.IsDir() {
					if entry.Name() == ".git" {
						return filepath.SkipDir
					}
					return nil
				}
				files++
				if files > 5000 {
					return fmt.Errorf("scan exceeds 5000 files")
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				data, err := ReadFile(root, relative, 8*1024*1024)
				if err != nil {
					return err
				}
				total += int64(len(data))
				if total > 64*1024*1024 {
					return fmt.Errorf("scan exceeds 64 MiB")
				}
				result.Files++
				report.InputHashes[filepath.ToSlash(relative)] = engineering.Hash(string(data))
				for _, label := range labels {
					marker := probe.Markers[label]
					le, be := make([]byte, 0, len(marker)*2), make([]byte, 0, len(marker)*2)
					for i := range len(marker) {
						le = append(le, marker[i], 0)
						be = append(be, 0, marker[i])
					}
					patterns := []struct {
						encoding string
						data     []byte
					}{
						{"literal", []byte(marker)}, {"base64", []byte(base64.StdEncoding.EncodeToString([]byte(marker)))}, {"url", []byte(url.QueryEscape(marker))}, {"utf16le", le}, {"utf16be", be},
					}
					seen := map[string]bool{}
					for _, variant := range patterns {
						pattern := variant.data
						if seen[string(pattern)] {
							continue
						}
						seen[string(pattern)] = true
						newline := []byte{'\n'}
						if variant.encoding == "utf16le" {
							newline = []byte{'\n', 0}
						}
						if variant.encoding == "utf16be" {
							newline = []byte{0, '\n'}
						}
						line := 1
						for start := 0; start < len(data); {
							if err := ctx.Err(); err != nil {
								return err
							}
							offset := bytes.Index(data[start:], pattern)
							if offset < 0 {
								break
							}
							offset += start
							line += bytes.Count(data[start:offset], newline)
							if len(report.Matches) >= 10000 {
								return fmt.Errorf("scan exceeds 10000 matches")
							}
							result.Counts[label]++
							report.Matches = append(report.Matches, BoundaryMatch{Path: filepath.ToSlash(relative), Label: label, Encoding: variant.encoding, Offset: offset, Line: line})
							start = offset + len(pattern)
						}
					}
				}
				return nil
			})
		}
		if err != nil {
			result.Passed = false
			result.Reason = err.Error()
		}
		if result.Files == 0 {
			result.Passed = false
			if result.Reason == "" {
				result.Reason = "no readable files in target"
			}
		}
		for _, count := range result.Counts {
			if target.Expect == "absent" && count > 0 || target.Expect == "present" && count == 0 {
				result.Passed = false
			}
		}
		if !result.Passed && result.Reason == "" {
			result.Reason = "marker presence differs from declared boundary"
		}
		report.Passed = report.Passed && result.Passed
		report.Results = append(report.Results, result)
	}
	// Passing absence claims require positive controls, so tests that never
	// injected a marker cannot silently pass.
	controls := map[string]bool{}
	for _, result := range report.Results {
		if result.Expect == "present" && result.Passed {
			for label, count := range result.Counts {
				if count > 0 {
					controls[label] = true
				}
			}
		}
	}
	for i := range report.Results {
		result := &report.Results[i]
		if result.Expect != "absent" {
			continue
		}
		for label := range result.Counts {
			if !controls[label] {
				result.Passed = false
				report.Passed = false
				if result.Reason == "" {
					result.Reason = "positive control not observed for marker: " + label
				}
				break
			}
		}
	}
	return report, nil
}
