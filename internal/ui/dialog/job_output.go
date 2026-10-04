package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type (
	jobOutputMsg struct {
		id    string
		lines []string
		done  bool
	}
	jobOutputTick struct{ id string }
)

func (d *Jobs) outputCmd(id string) tea.Cmd {
	ws := d.com.Workspace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		reader, ok := ws.(workspace.BackgroundOutputReader)
		if !ok {
			return jobOutputMsg{id: id, done: true, lines: []string{d.com.Text("This workspace does not expose process output")}}
		}
		out, err := reader.BackgroundJobOutput(ctx, id)
		if err != nil {
			return jobOutputMsg{id: id, done: true, lines: []string{d.com.Text("Output unavailable: ") + err.Error()}}
		}
		lines := []string{fmt.Sprintf("Finished=%t | truncated=%t", out.Done, out.Truncated), d.com.Text("Stdout:")}
		lines = append(lines, strings.Split(out.Stdout, "\n")...)
		lines = append(lines, d.com.Text("Stderr:"))
		lines = append(lines, strings.Split(out.Stderr, "\n")...)
		if out.Error != "" {
			lines = append(lines, d.com.Text("Exit error: ")+out.Error)
		}
		if len(lines) > 1024 {
			lines = append([]string{d.com.Text("Showing final 1024 output lines")}, lines[len(lines)-1023:]...)
		}
		return jobOutputMsg{id: id, lines: lines, done: out.Done}
	}
}
