package environment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

func Probe(requirement ToolRequirement) ([]string, error) {
	switch requirement.Name {
	case "go":
		return []string{"go", "version"}, nil
	case "node", "python", "python3", "rustc", "uv", "npm", "pnpm", "yarn", "bun":
		return []string{requirement.Name, "--version"}, nil
	}
	return nil, fmt.Errorf("unsupported tool version probe")
}

func ObservedVersion(requirement ToolRequirement, output string) (string, error) {
	if len(output) > 8192 {
		return "", fmt.Errorf("version output exceeds limit")
	}
	line := strings.TrimSpace(strings.Split(output, "\n")[0])
	fields := strings.Fields(line)
	version := line
	switch requirement.Name {
	case "go":
		if len(fields) < 3 || fields[0] != "go" || fields[1] != "version" {
			return "", fmt.Errorf("Go version was not observed")
		}
		version = strings.TrimPrefix(fields[2], "go")
	case "python", "python3":
		if len(fields) != 2 || fields[0] != "Python" {
			return "", fmt.Errorf("Python version was not observed")
		}
		version = fields[1]
	case "rustc", "uv":
		if len(fields) < 2 || fields[0] != requirement.Name {
			return "", fmt.Errorf("tool version was not observed")
		}
		version = fields[1]
	}
	version = strings.TrimPrefix(version, "v")
	if !semver.IsValid("v" + version) {
		return "", fmt.Errorf("version output is unavailable or unsupported")
	}
	matched, err := MatchesVersion(version, requirement.Constraint)
	if err != nil {
		return "", err
	}
	if !matched {
		return version, fmt.Errorf("observed %s does not satisfy %s", version, requirement.Constraint)
	}
	return version, nil
}

func MatchesVersion(version, constraint string) (bool, error) {
	version = semver.Canonical("v" + strings.TrimPrefix(version, "v"))
	if version == "" {
		return false, fmt.Errorf("invalid observed version")
	}
	if len(constraint) > 256 {
		return false, fmt.Errorf("version constraint exceeds limit")
	}
	if strings.TrimSpace(constraint) == "" || constraint == "*" {
		return true, nil
	}
	if semver.Prerelease(version) != "" && !strings.Contains(constraint, "-") {
		return false, fmt.Errorf("prerelease version requires an explicit compatible constraint")
	}
	constraint = regexp.MustCompile(`([<>=~^]+)\s+`).ReplaceAllString(constraint, "$1")
	for _, branch := range strings.Split(constraint, "||") {
		matches := true
		for _, token := range strings.Fields(strings.ReplaceAll(branch, ",", " ")) {
			ok, err := matchConstraint(version, token)
			if err != nil {
				return false, err
			}
			matches = matches && ok
		}
		if matches && strings.TrimSpace(branch) != "" {
			return true, nil
		}
	}
	return false, nil
}

func matchConstraint(version, token string) (bool, error) {
	op := ""
	for _, candidate := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(token, candidate) {
			op, token = candidate, strings.TrimPrefix(token, candidate)
			break
		}
	}
	token = strings.TrimPrefix(token, "v")
	if token == "*" || token == "x" {
		return op == "" || op == "=", nil
	}
	token = strings.TrimSuffix(strings.TrimSuffix(token, ".x"), ".*")
	base := semver.Canonical("v" + token)
	if base == "" {
		return false, fmt.Errorf("unsupported version constraint")
	}
	comparison := semver.Compare(version, base)
	switch op {
	case ">=":
		return comparison >= 0, nil
	case "<=":
		return comparison <= 0, nil
	case ">":
		return comparison > 0, nil
	case "<":
		return comparison < 0, nil
	case "=":
		return comparison == 0, nil
	}
	parts := strings.Split(strings.TrimPrefix(base, "v"), ".")
	if len(parts) != 3 || strings.ContainsAny(parts[2], "-+") {
		if op == "" {
			return comparison == 0, nil
		}
		return false, fmt.Errorf("unsupported prerelease range")
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])
	upper := ""
	switch op {
	case "^":
		if major > 0 {
			upper = fmt.Sprintf("v%d.0.0", major+1)
		} else if minor > 0 {
			upper = fmt.Sprintf("v0.%d.0", minor+1)
		} else {
			upper = fmt.Sprintf("v0.0.%d", patch+1)
		}
	case "~":
		if strings.Count(token, ".") == 0 {
			upper = fmt.Sprintf("v%d.0.0", major+1)
		} else {
			upper = fmt.Sprintf("v%d.%d.0", major, minor+1)
		}
	case "":
		switch strings.Count(token, ".") {
		case 0:
			upper = fmt.Sprintf("v%d.0.0", major+1)
		case 1:
			upper = fmt.Sprintf("v%d.%d.0", major, minor+1)
		default:
			return comparison == 0, nil
		}
	default:
		return false, fmt.Errorf("unsupported version comparator")
	}
	return comparison >= 0 && semver.Compare(version, upper) < 0, nil
}
