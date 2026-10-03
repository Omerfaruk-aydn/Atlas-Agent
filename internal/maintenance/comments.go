package maintenance

import (
	"context"
	"fmt"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/xchroma"
	"github.com/alecthomas/chroma/v2"
)

type CommentTask struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Prompt string `json:"prompt"`
}

// CommentTasks uses lexer comment tokens, never string literals as directives.
func CommentTasks(ctx context.Context, path string, data []byte) ([]CommentTask, error) {
	if len(data) > 512*1024 {
		return nil, fmt.Errorf("watched source exceeds 512KiB")
	}
	lexer := xchroma.MatchLexer(path)
	if lexer == nil {
		return nil, fmt.Errorf("no comment lexer handles %s", path)
	}
	iterator, err := lexer.Tokenise(nil, string(data))
	if err != nil {
		return nil, err
	}
	line := 1
	tasks := []CommentTask{}
	seen := map[string]bool{}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if token.Type.InCategory(chroma.Comment) {
			for offset, value := range strings.Split(token.Value, "\n") {
				value = strings.TrimSpace(value)
				for _, prefix := range []string{"//", "#", "/*", "*"} {
					value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
				}
				value = strings.TrimSpace(strings.TrimSuffix(value, "*/"))
				if !strings.HasPrefix(value, "ATLAS:") {
					continue
				}
				prompt := strings.TrimSpace(strings.TrimPrefix(value, "ATLAS:"))
				if prompt == "" || len(prompt) > 4096 {
					return nil, fmt.Errorf("comment task must contain 1-4096 bytes")
				}
				id := engineering.Hash(path + "\x00" + prompt)
				if !seen[id] {
					tasks = append(tasks, CommentTask{ID: id, Path: path, Line: line + offset, Prompt: prompt})
					seen[id] = true
				}
				if len(tasks) > 64 {
					return nil, fmt.Errorf("comment task count exceeds 64")
				}
			}
		}
		line += strings.Count(token.Value, "\n")
	}
	return tasks, nil
}
