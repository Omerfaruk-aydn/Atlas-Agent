package model

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ansiext"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ansi"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type workflowInput struct {
	mode, text, task, file string
	line, cursor           int
}

func (i workflowInput) visible(width int) string {
	runes := []rune(i.text)
	cursor := min(max(0, i.cursor), len(runes))
	oneLine := func(s string) string {
		return ansiext.Escape(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s))
	}
	before := oneLine(string(runes[:cursor]))
	text := before + "│" + oneLine(string(runes[cursor:]))
	start := max(0, ansi.StringWidth(before)-max(1, width/2))
	return ansi.Cut(text, start, start+max(0, width))
}

func (i workflowInput) label() string {
	target := i.task
	if target == "" {
		target = "coordinator"
	}
	switch i.mode {
	case "context_pin":
		return "Pin project context file (literal relative path; maximum 16 files)"
	case "queue":
		return "Queue for " + target + " ([dep1,dep2] optional prefix)"
	case "team_limit":
		return "Maximum concurrent agents (1-16)"
	case "budget":
		return "Session limits JSON (max_tokens, max_tool_calls, max_cost_usd, max_duration_ms)"
	case "reassign":
		return "Specialist role for " + target
	case "task_replan":
		return "Revised task description for " + target
	case "task_scope":
		return "Owned paths for " + target + " (comma separated)"
	case "feedback":
		return fmt.Sprintf("Review %s:%d for %s", i.file, i.line, target)
	default:
		return "Instruction for " + target
	}
}

func (i *workflowInput) insert(text string) {
	if len(i.text)+len(text) > 8192 {
		return
	}
	runes := []rune(i.text)
	i.cursor = min(max(0, i.cursor), len(runes))
	i.text = string(runes[:i.cursor]) + text + string(runes[i.cursor:])
	i.cursor += len([]rune(text))
}

func (m *UI) handleWorkflowInput(key tea.KeyPressMsg) tea.Cmd {
	p := &m.workflow
	i := &p.input
	runes := []rune(i.text)
	i.cursor = min(max(0, i.cursor), len(runes))
	switch key.String() {
	case "esc":
		p.input = workflowInput{}
	case "left":
		i.cursor = max(0, i.cursor-1)
	case "right":
		i.cursor = min(len(runes), i.cursor+1)
	case "home", "ctrl+a":
		i.cursor = 0
	case "end", "ctrl+e":
		i.cursor = len(runes)
	case "ctrl+u":
		i.text, i.cursor = "", 0
	case "backspace":
		if i.cursor > 0 {
			i.text = string(runes[:i.cursor-1]) + string(runes[i.cursor:])
			i.cursor--
		}
	case "delete":
		if i.cursor < len(runes) {
			i.text = string(runes[:i.cursor]) + string(runes[i.cursor+1:])
		}
	case "enter":
		if p.loading {
			return nil
		}
		control, err := i.control()
		if err != nil {
			p.err = err.Error()
			return nil
		}
		p.input = workflowInput{}
		return m.workflowControlCmd(control)
	default:
		text := key.Text
		if text == "" && len([]rune(key.String())) == 1 {
			text = key.String()
		}
		i.insert(text)
	}
	return nil
}

func (i workflowInput) control() (engineering.WorkflowControl, error) {
	control := engineering.WorkflowControl{Action: i.mode, TaskID: i.task, Text: strings.TrimSpace(i.text), File: i.file, Line: i.line}
	if control.Text == "" {
		return control, fmt.Errorf("enter an instruction or value")
	}
	switch i.mode {
	case "task_scope":
		for _, entry := range strings.Split(control.Text, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				control.OwnedPaths = append(control.OwnedPaths, entry)
			}
		}
		control.Text = ""
	case "reassign":
		control.Agent = control.Text
		control.Text = ""
	case "team_limit":
		n, err := strconv.Atoi(control.Text)
		if err != nil || n < 1 || n > 16 {
			return control, fmt.Errorf("enter an agent limit between 1 and 16")
		}
		control.MaxAgents, control.Text = n, ""
	case "budget":
		limits := new(engineering.Limits)
		decoder := json.NewDecoder(strings.NewReader(control.Text))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(limits); err != nil {
			return control, fmt.Errorf("invalid budget JSON: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return control, fmt.Errorf("budget must contain exactly one JSON object")
		}
		if err := limits.Validate(); err != nil {
			return control, err
		}
		control.Limits, control.Text = limits, ""
	case "queue":
		if strings.HasPrefix(control.Text, "[") {
			end := strings.Index(control.Text, "]")
			if end < 0 {
				return control, fmt.Errorf("close the dependency prefix with ]")
			}
			for _, dependency := range strings.Split(control.Text[1:end], ",") {
				if dependency = strings.TrimSpace(dependency); dependency != "" {
					control.DependsOn = append(control.DependsOn, dependency)
				}
			}
			control.Text = strings.TrimSpace(control.Text[end+1:])
			if control.Text == "" {
				return control, fmt.Errorf("enter queue instruction after dependencies")
			}
		}
	}
	return control, nil
}

func (m *UI) workflowWorkspaceKey(key string) (bool, tea.Cmd) {
	p := &m.workflow
	if p.tab == workflowInteractions && (key == "v" || key == "f") {
		action := "interaction_capture"
		if key == "f" {
			action = "interaction_preview"
		}
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action})
	}
	if key == "i" {
		p.tab, p.selected, p.timeline, p.details = workflowInteractions, 0, false, false
		p.detailOffset = 0
		return true, nil
	}
	if p.tab == workflowInteractions && (key == "p" || key == "r") {
		action := "interaction_pause"
		if key == "r" {
			action = "interaction_resume"
		}
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action})
	}
	if key == "0" {
		p.tab, p.selected, p.timeline, p.details = workflowBatches, 0, false, false
		p.detailOffset = 0
		return true, nil
	}
	if key >= "1" && key <= "9" && len(key) == 1 {
		p.tab, p.selected, p.timeline, p.details = int(key[0]-'1'), 0, false, false
		p.detailOffset = 0
		return true, nil
	}
	if p.timeline {
		return false, nil
	}
	row, selected := p.selectedRow()
	if selected && p.tab == workflowAutomation {
		action := map[string]string{"p": "job_pause", "r": "job_resume", "x": "job_recover"}[key]
		if action != "" {
			return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, Text: row.ID})
		}
	}
	if selected && p.tab == workflowDurableBoard {
		action := map[string]string{"a": "board_accept", "r": "board_retry", "x": "board_recover"}[key]
		if action != "" {
			return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, Text: row.ID})
		}
	}
	if p.tab == workflowBatches && key == "f" && selected {
		for _, batch := range p.snapshot.Batches {
			for _, item := range batch.Rows {
				if row.ID == batch.ID+"/"+item.ID {
					return true, m.workflowControlCmd(engineering.WorkflowControl{Action: "batch_retry", Text: batch.ID})
				}
			}
		}
		return true, nil
	}
	if p.tab == workflowContext {
		if key == "f" {
			p.input = workflowInput{mode: "context_pin"}
			return true, nil
		}
		if key == "x" && selected {
			for _, entry := range p.snapshot.Context.Entries {
				if entry.ID != row.ID {
					continue
				}
				action := "context_exclude"
				if entry.Excluded {
					action = "context_include"
				}
				if entry.Kind == "pinned" {
					action = "context_unpin"
				} else if entry.Kind != "tool-result" {
					p.err = "Only optional pinned files and tool results can be removed"
					return true, nil
				}
				return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, Text: entry.ID})
			}
		}
	}
	task := row.TaskID
	switch key {
	case "e", "E", "n", "N", "a", "l", "b", "v", "w":
		mode := "steer"
		if key == "E" || key == "N" {
			task = ""
		}
		if key == "n" || key == "N" {
			mode = "queue"
		}
		if key == "a" {
			if task == "" {
				p.err = "Select a task to assign"
				return true, nil
			}
			mode = "reassign"
		}
		if key == "l" {
			mode, task = "team_limit", ""
		}
		if key == "b" {
			mode, task = "budget", ""
		}
		if key == "v" || key == "w" {
			if task == "" {
				p.err = "Select a task to revise"
				return true, nil
			}
			mode = "task_replan"
			if key == "w" {
				mode = "task_scope"
			}
		}
		p.input = workflowInput{mode: mode, task: task}
		if mode == "budget" {
			data, _ := json.Marshal(p.snapshot.Limits)
			p.input.text = string(data)
			p.input.cursor = len([]rune(p.input.text))
		}
		return true, nil
	case "f":
		if task == "" {
			p.err = "Select a reconciled task to retry"
			return true, nil
		}
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: "task_retry", TaskID: task})
	case "h", "u", "x":
		if task == "" {
			p.err = "Select a task"
			return true, nil
		}
		action := "task_pause"
		if key == "u" {
			action = "task_resume"
		}
		if key == "x" {
			action = "cancel_task"
		}
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, TaskID: task})
	case "+", "-", "delete", "y", "z":
		if p.tab != workflowQueue || !selected {
			return false, nil
		}
		action := map[string]string{"+": "queue_down", "-": "queue_up", "delete": "queue_cancel", "y": "queue_retry", "z": "queue_remove"}[key]
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, DirectiveID: row.ID})
	case "i", "R":
		if p.tab != workflowCheckpoints || !selected {
			return false, nil
		}
		action := "inspect_checkpoint"
		if key == "R" {
			action = "resume_checkpoint"
		}
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: action, CheckpointID: row.ID})
	case "c":
		return true, m.workflowControlCmd(engineering.WorkflowControl{Action: "start_queue"})
	case "d":
		return true, m.workflowFilesCmd()
	case "o":
		return true, m.openJobsDialog()
	case "g":
		return true, m.openAgentHubDialog()
	case "enter":
		if p.tab == workflowAttention && selected {
			p.tab, p.selected, p.details = row.Destination, 0, true
			for j, target := range p.rows() {
				if target.ID == row.ID || row.TaskID != "" && target.TaskID == row.TaskID {
					p.selected = j
					break
				}
			}
			return true, nil
		}
	}
	return false, nil
}
