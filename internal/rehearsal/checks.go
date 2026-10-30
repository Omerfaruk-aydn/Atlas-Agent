package rehearsal

import (
	"regexp"
	"sort"
	"strings"
)

// Recognisers for failing tests in the output of common runners. They only
// need to be stable between two runs of the same command, because the
// rehearsal compares the names before and after the change.
var failedTestPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`),                        // go test
	regexp.MustCompile(`(?m)^FAIL\s+(\S+)`),                              // go package or build failure
	regexp.MustCompile(`(?m)^FAILED (\S+)`),                              // pytest
	regexp.MustCompile(`(?m)^test (\S+) \.\.\. FAILED`),                  // cargo
	regexp.MustCompile(`(?m)^\s*(?:✕|×|✗)\s+(.+?)(?:\s+\(\d+ ?m?s\))?$`), // jest, vitest
}

// FailedTests extracts the names of failing tests from runner output.
func FailedTests(output string) []string {
	seen := map[string]bool{}
	for _, pattern := range failedTestPatterns {
		for _, m := range pattern.FindAllStringSubmatch(output, -1) {
			if name := strings.TrimSpace(m[1]); name != "" {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 50 {
		names = names[:50]
	}
	return names
}
