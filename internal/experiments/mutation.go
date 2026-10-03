package experiments

import (
	"encoding/json"
	"fmt"
)

type MutationSummary struct {
	Killed     int      `json:"killed"`
	Survived   int      `json:"survived"`
	NoCoverage int      `json:"no_coverage"`
	Timeout    int      `json:"timeout"`
	Invalid    int      `json:"invalid"`
	Pending    int      `json:"pending"`
	Score      *float64 `json:"score,omitempty"`
}

// MutationReport reads the mutation-testing-elements JSON report format.
func MutationReport(data []byte) (MutationSummary, error) {
	var report struct {
		SchemaVersion string `json:"schemaVersion"`
		Files         map[string]struct {
			Mutants []struct {
				Status string `json:"status"`
			} `json:"mutants"`
		} `json:"files"`
	}
	var result MutationSummary
	if len(data) > 512*1024 {
		return result, fmt.Errorf("mutation report exceeds 512KiB")
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return result, err
	}
	if report.SchemaVersion == "" || len(report.Files) == 0 || len(report.Files) > 4096 {
		return result, fmt.Errorf("invalid mutation report schema or file count")
	}
	total := 0
	for _, file := range report.Files {
		for _, mutant := range file.Mutants {
			total++
			if total > 10000 {
				return result, fmt.Errorf("mutation count exceeds 10000")
			}
			switch mutant.Status {
			case "Killed":
				result.Killed++
			case "Survived":
				result.Survived++
			case "NoCoverage":
				result.NoCoverage++
			case "Timeout":
				result.Timeout++
			case "CompileError", "RuntimeError", "Ignored":
				result.Invalid++
			case "Pending":
				result.Pending++
			default:
				return result, fmt.Errorf("unknown mutant status %q", mutant.Status)
			}
		}
	}
	if total == 0 {
		return result, fmt.Errorf("mutation report contains no mutants")
	}
	denominator := result.Killed + result.Timeout + result.Survived + result.NoCoverage
	if denominator > 0 {
		score := 100 * float64(result.Killed+result.Timeout) / float64(denominator)
		result.Score = &score
	}
	return result, nil
}
