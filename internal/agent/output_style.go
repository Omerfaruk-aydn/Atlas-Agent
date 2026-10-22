package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
)

const maxOutputStyleBytes = 16 << 10

var outputStyleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

var builtinOutputStyles = map[string]string{
	"default":     "Use the standard engineering response format: concise outcome, evidence and remaining risks.",
	"concise":     "Be brief. Lead with the result, omit repetition, and include only material verification and risks.",
	"explanatory": "Explain important decisions and trade-offs with concrete evidence. Keep sections focused and avoid unnecessary implementation trivia.",
	"learning":    "Teach while working: explain unfamiliar concepts, why each significant step matters, and how the user can verify the result.",
}

// outputStyle resolves a configured built-in or a project/user Markdown file.
// Project files intentionally override user files, never built-ins.
func outputStyle(cfg *config.Config, workingDir string) (string, error) {
	if cfg == nil || cfg.Options == nil || cfg.Options.SafeMode {
		return "", nil
	}
	name := strings.ToLower(strings.TrimSpace(cfg.Options.OutputStyle))
	if name == "" || name == "default" {
		return builtinOutputStyles["default"], nil
	}
	if !outputStyleName.MatchString(name) {
		return "", fmt.Errorf("invalid output style name %q", name)
	}
	if text, ok := builtinOutputStyles[name]; ok {
		return text, nil
	}
	userDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	paths := []string{
		filepath.Join(workingDir, ".atlas", "output-styles", name+".md"),
		filepath.Join(userDir, "Atlas-Agent", "output-styles", name+".md"),
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > maxOutputStyleBytes {
			return "", fmt.Errorf("output style %s must be a regular file no larger than 16 KiB", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", fmt.Errorf("output style %s is empty", name)
		}
		return text, nil
	}
	return "", fmt.Errorf("output style %q was not found in project or user output-styles directories", name)
}
