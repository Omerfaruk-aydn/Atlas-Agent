package session

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
)

func TodosFingerprint(todos []Todo) string {
	data, _ := marshalTodos(todos)
	return engineering.Hash(data)
}

// CompareAndSwapTodos changes only the task graph and preserves concurrent
// usage/title updates. The SQL predicate guards against other graph writers.
func (s *service) CompareAndSwapTodos(ctx context.Context, id, expectedFingerprint string, todos []Todo) (Session, error) {
	raw, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, err
	}
	previous := s.fromDBItem(raw)
	if TodosFingerprint(previous.Todos) != expectedFingerprint {
		return Session{}, fmt.Errorf("task graph revision conflict")
	}
	if err := ValidateTaskGraph(todos); err != nil {
		return Session{}, err
	}
	for _, todo := range todos {
		if err := ValidateTodo(todo); err != nil {
			return Session{}, err
		}
	}
	next := previous
	next.Todos = todos
	if s.completionGate != nil {
		if err := s.completionGate(ctx, previous, next); err != nil {
			return Session{}, err
		}
	}
	encoded, err := marshalTodos(todos)
	if err != nil {
		return Session{}, err
	}
	rows, err := s.q.CompareAndSwapSessionTodos(ctx, db.CompareAndSwapSessionTodosParams{ID: id, ExpectedTodos: raw.Todos, NewTodos: sql.NullString{String: encoded, Valid: encoded != ""}})
	if err != nil {
		return Session{}, err
	}
	if rows != 1 {
		return Session{}, fmt.Errorf("task graph revision conflict")
	}
	saved, err := s.Get(ctx, id)
	if err != nil {
		return Session{}, err
	}
	s.Publish(pubsub.UpdatedEvent, saved)
	return saved, nil
}
