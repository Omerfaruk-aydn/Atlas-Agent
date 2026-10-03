package model

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
)

type workflowOutputLoaded struct {
	epoch, request         uint64
	sessionID, operationID string
	lines                  []string
}

func (m *UI) workflowOutputCmd() tea.Cmd {
	p := &m.workflow
	row, selected := p.selectedRow()
	if p.tab != workflowOperations || !selected || !p.details || m.session == nil {
		return nil
	}
	i := slices.IndexFunc(p.snapshot.Operations, func(op engineering.Operation) bool { return op.ID == row.ID })
	if i < 0 {
		return nil
	}
	op := p.snapshot.Operations[i]
	id, epoch := m.session.ID, p.epoch
	p.outputRequest++
	request := p.outputRequest
	ws := m.com.Workspace
	sessions := []string{id}
	for _, run := range p.snapshot.Executions {
		if run.TaskID == op.TaskID {
			sessions = append(sessions, run.SessionIDs...)
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		lines := []string{"Actual output | PgUp/PgDown scroll"}
		if op.BackgroundID != "" {
			reader, ok := ws.(workspace.BackgroundOutputReader)
			if !ok {
				lines = append(lines, "This workspace does not expose background output")
			} else {
				out, err := reader.BackgroundJobOutput(ctx, op.BackgroundID)
				if err != nil {
					lines = append(lines, "Output error: "+err.Error())
				} else {
					lines = append(lines, fmt.Sprintf("Process finished=%t; output truncated=%t", out.Done, out.Truncated), "Stdout:")
					lines = append(lines, strings.Split(out.Stdout, "\n")...)
					lines = append(lines, "Stderr:")
					lines = append(lines, strings.Split(out.Stderr, "\n")...)
					if out.Error != "" {
						lines = append(lines, "Exit error: "+out.Error)
					}
				}
			}
		} else {
			found := false
			for _, sid := range sessions {
				messages, err := ws.ListMessages(ctx, sid)
				if err != nil {
					lines = append(lines, "History error: "+err.Error())
					continue
				}
				for _, msg := range messages[max(0, len(messages)-2000):] {
					for _, result := range msg.ToolResults() {
						if result.ToolCallID != op.CallID {
							continue
						}
						text := result.Content
						if len(text) > 64*1024 {
							text = strings.ToValidUTF8(text[len(text)-64*1024:], "�")
							lines = append(lines, "Showing final 64 KiB of persisted output")
						}
						lines = append(lines, fmt.Sprintf("Persisted tool result %s error=%t", result.Name, result.IsError))
						lines = append(lines, strings.Split(text, "\n")...)
						found = true
					}
				}
				if found {
					break
				}
			}
			if !found {
				lines = append(lines, "Output has not been persisted for this operation; inspect its live agent or background job")
			}
		}
		if len(lines) > 1024 {
			lines = append([]string{"Showing final 1024 output lines"}, lines[len(lines)-1023:]...)
		}
		return workflowOutputLoaded{epoch: epoch, request: request, sessionID: id, operationID: op.ID, lines: lines}
	}
}
