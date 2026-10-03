// Package agentstate stores bounded agent runtime records in SQLite.
package agentstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
)

var ErrConflict = errors.New("record changed; refresh and retry")

type (
	Store  struct{ DB *sql.DB }
	Record struct {
		Key      string          `json:"key"`
		Revision int64           `json:"revision"`
		Payload  json.RawMessage `json:"payload"`
	}
)

func (s *Store) Get(ctx context.Context, namespace, key string, out any) (int64, error) {
	var rev int64
	var payload string
	err := s.DB.QueryRowContext(ctx, db.AgentStateQuery("get"), namespace, key).Scan(&rev, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return rev, json.Unmarshal([]byte(payload), out)
}

func (s *Store) Put(ctx context.Context, namespace, key string, expected int64, value any) error {
	if namespace == "" || key == "" || len(namespace) > 256 || len(key) > 256 || strings.ContainsRune(key, 0) {
		return errors.New("invalid state key")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) > 256*1024 {
		return errors.New("state record exceeds 256 KiB")
	}
	if expected == 0 {
		result, err := s.DB.ExecContext(ctx, db.AgentStateQuery("create"), namespace, key, string(payload), time.Now().UnixMilli(), namespace, namespace)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return nil
	}
	result, err := s.DB.ExecContext(ctx, db.AgentStateQuery("put"), string(payload), time.Now().UnixMilli(), namespace, key, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func (s *Store) List(ctx context.Context, namespace string) ([]Record, error) {
	rows, err := s.DB.QueryContext(ctx, db.AgentStateQuery("list"), namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []Record{}
	for rows.Next() {
		var r Record
		var p string
		if err := rows.Scan(&r.Key, &r.Revision, &p); err != nil {
			return nil, err
		}
		r.Payload = json.RawMessage(p)
		records = append(records, r)
	}
	return records, rows.Err()
}

func Update[T any](ctx context.Context, s *Store, ns, key string, change func(*T) error) error {
	for range 8 {
		var value T
		rev, err := s.Get(ctx, ns, key, &value)
		if err != nil {
			return err
		}
		if err = change(&value); err != nil {
			return err
		}
		err = s.Put(ctx, ns, key, rev, value)
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return fmt.Errorf("state update: %w", ErrConflict)
}

type Goal struct {
	RunID     string `json:"run_id"`
	Text      string `json:"text"`
	Budget    int    `json:"budget"`
	Used      int    `json:"used"`
	Claimed   bool   `json:"claimed"`
	Done      bool   `json:"done"`
	Stopped   bool   `json:"stopped"`
	Reason    string `json:"reason,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}
