package engineering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

type UserDirective struct {
	ID         string   `json:"id"`
	TaskID     string   `json:"task_id,omitempty"`
	Mode       string   `json:"mode"`
	Text       string   `json:"text"`
	DependsOn  []string `json:"depends_on,omitempty"`
	Status     string   `json:"status"`
	CreatedAt  int64    `json:"created_at"`
	ReceivedAt int64    `json:"received_at,omitempty"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
}

type ControlBoard struct {
	Directives    []UserDirective `json:"directives"`
	HeldTasks     []string        `json:"held_tasks,omitempty"`
	MaxAgents     int             `json:"max_agents,omitempty"`
	ResumePreview *ResumePlan     `json:"resume_preview,omitempty"`
}

func controlNamespace(id string) string { return "workflow-controls-" + Hash(id) }

func (s *Store) ReadControlBoard(ctx context.Context, id string) (Record, ControlBoard, error) {
	record, data, err := s.ReadRecord(ctx, controlNamespace(id), "board")
	var board ControlBoard
	if errors.Is(err, os.ErrNotExist) {
		return record, board, nil
	}
	if err != nil {
		return record, board, err
	}
	if err := json.Unmarshal(data, &board); err != nil {
		return record, board, err
	}
	return record, board, board.validate()
}

func (board ControlBoard) validate() error {
	if len(board.Directives) > 256 || len(board.HeldTasks) > 256 || board.MaxAgents < 0 || board.MaxAgents > 16 {
		return fmt.Errorf("invalid control board bounds")
	}
	for _, id := range board.HeldTasks {
		if !validID(id) {
			return fmt.Errorf("invalid held task")
		}
	}
	seen := map[string]bool{}
	for _, directive := range board.Directives {
		if _, err := uuid.Parse(directive.ID); err != nil || seen[directive.ID] || strings.TrimSpace(directive.Text) == "" || len(directive.Text) > 8192 || len(directive.DependsOn) > 32 || directive.Line < 0 || len(directive.File) > 512 {
			return fmt.Errorf("invalid directive record")
		}
		seen[directive.ID] = true
		if directive.Mode != "steer" && directive.Mode != "next" {
			return fmt.Errorf("invalid directive mode")
		}
		if !slices.Contains([]string{"queued", "claimed", "received", "cancelled"}, directive.Status) {
			return fmt.Errorf("invalid directive status")
		}
		if directive.TaskID != "" && !validID(directive.TaskID) {
			return fmt.Errorf("invalid directive task")
		}
		for _, dep := range directive.DependsOn {
			if !validID(dep) {
				return fmt.Errorf("invalid directive dependency")
			}
		}
		file := strings.ReplaceAll(directive.File, `\`, "/")
		if file != "" && (directive.Line < 1 || strings.HasPrefix(file, "/") || strings.ContainsAny(file, "*?:") || path.Clean(file) == "." || path.Clean(file) == ".." || strings.HasPrefix(path.Clean(file), "../")) {
			return fmt.Errorf("invalid feedback location")
		}
	}
	return nil
}

func (s *Store) UpdateControlBoard(ctx context.Context, id string, change func(*ControlBoard) error) error {
	release, err := s.WorkflowLock(ctx, "controls:"+id)
	if err != nil {
		return err
	}
	defer release()
	record, board, err := s.ReadControlBoard(ctx, id)
	if err != nil {
		return err
	}
	if err := change(&board); err != nil {
		return err
	}
	if len(board.Directives) > 256 {
		return fmt.Errorf("control queue is full; remove completed entries before adding more")
	}
	if err := board.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(board)
	if err != nil {
		return err
	}
	_, err = s.PutRecordStrict(ctx, controlNamespace(id), "board", record.Revision, data)
	return err
}

func (s *Store) EnqueueDirective(ctx context.Context, id string, directive UserDirective) error {
	if strings.TrimSpace(directive.Text) == "" || len(directive.Text) > 8192 || len(directive.DependsOn) > 32 {
		return fmt.Errorf("directive requires bounded text and dependencies")
	}
	if directive.Mode != "steer" && directive.Mode != "next" {
		return fmt.Errorf("directive mode must be steer or next")
	}
	directive.ID, directive.Status, directive.CreatedAt = uuid.NewString(), "queued", time.Now().UnixMilli()
	return s.UpdateControlBoard(ctx, id, func(board *ControlBoard) error { board.Directives = append(board.Directives, directive); return nil })
}

// ClaimDirective records delivery before any message is created. A crash leaves
// an inspectable claim, never an automatically replayed instruction.
func (s *Store) ClaimDirective(ctx context.Context, id, task string, firstStep bool, completed map[string]bool) (*UserDirective, error) {
	_, board, err := s.ReadControlBoard(ctx, id)
	if err != nil {
		return nil, err
	}
	eligible := func(d UserDirective) bool {
		if d.Status != "queued" || d.TaskID != task || d.Mode == "next" && !firstStep {
			return false
		}
		for _, dependency := range d.DependsOn {
			if !completed[dependency] {
				return false
			}
		}
		return true
	}
	if !slices.ContainsFunc(board.Directives, eligible) {
		return nil, nil
	}
	var claimed *UserDirective
	err = s.UpdateControlBoard(ctx, id, func(board *ControlBoard) error {
		for i := range board.Directives {
			if eligible(board.Directives[i]) {
				board.Directives[i].Status = "claimed"
				copy := board.Directives[i]
				claimed = &copy
				break
			}
		}
		return nil
	})
	return claimed, err
}

func (s *Store) ReceiveDirective(ctx context.Context, id, directiveID string) error {
	return s.UpdateControlBoard(ctx, id, func(board *ControlBoard) error {
		for i := range board.Directives {
			if board.Directives[i].ID == directiveID {
				if board.Directives[i].Status != "claimed" {
					return fmt.Errorf("directive delivery state changed")
				}
				board.Directives[i].Status, board.Directives[i].ReceivedAt = "received", time.Now().UnixMilli()
				return nil
			}
		}
		return fmt.Errorf("directive not found")
	})
}
