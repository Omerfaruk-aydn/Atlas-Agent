package teams

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// The pure-Go SQLite driver, the same one the rest of the project uses.
	_ "modernc.org/sqlite"
)

// storeFile is the board database inside the directory given to SetStoreDir.
const storeFile = "board.db"

// pollInterval is how often a parked reader looks for messages another
// process wrote. Messages from this process wake readers at once.
const pollInterval = 250 * time.Millisecond

// store keeps a board's messages in SQLite so they survive a restart and can
// be shared by several processes. A message's number is assigned inside the
// database in one write transaction, so two processes posting at the same
// moment never get the same number.
type store struct {
	dir string

	mu   sync.Mutex
	db   *sql.DB
	idle *time.Timer
}

// idleClose is how long an unused database stays open. Closing it when the
// board is quiet releases the file handle, which matters on Windows where an
// open database cannot be deleted or moved.
const idleClose = 500 * time.Millisecond

// newStore remembers where the database lives. It is opened on first use, so
// a session that never touches the board never creates the file.
func newStore(dir string) *store { return &store{dir: dir} }

// handle returns the open database, opening it if needed, and restarts the
// idle timer.
func (s *store) handle() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		db, err := openDB(s.dir)
		if err != nil {
			return nil, err
		}
		s.db = db
	}
	if s.idle == nil {
		s.idle = time.AfterFunc(idleClose, s.closeIdle)
	} else {
		s.idle.Reset(idleClose)
	}
	return s.db, nil
}

func (s *store) closeIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
}

// openDB opens the board database under dir. Several processes may start at
// once, and switching a new file to WAL mode is not covered by the busy
// timeout, so a locked database is retried for a few seconds.
func openDB(dir string) (*sql.DB, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		db, err := openDBOnce(dir)
		if err == nil || !strings.Contains(err.Error(), "locked") || time.Now().After(deadline) {
			return db, err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// openDBOnce opens (creating if needed) the board database under dir.
func openDBOnce(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create board directory: %w", err)
	}
	// WAL lets readers and one writer work together across processes; the busy
	// timeout makes a second writer wait instead of failing.
	dsn := "file:" + filepath.ToSlash(filepath.Join(dir, storeFile)) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open board database: %w", err)
	}
	db.SetMaxOpenConns(4)
	const schema = `
CREATE TABLE IF NOT EXISTS messages (
	team    TEXT    NOT NULL,
	seq     INTEGER NOT NULL,
	from_id TEXT    NOT NULL,
	name    TEXT    NOT NULL,
	text    TEXT    NOT NULL,
	to_json TEXT    NOT NULL,
	ts      INTEGER NOT NULL,
	PRIMARY KEY (team, seq)
) WITHOUT ROWID;`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("prepare board database: %w", err)
	}
	return db, nil
}

// insert stores a message under the next number for its team and returns that
// number.
func (s *store) insert(team string, msg Message) (int, error) {
	to, err := json.Marshal(msg.To)
	if err != nil {
		return 0, err
	}
	db, err := s.handle()
	if err != nil {
		return 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE team = ?`, team).Scan(&seq); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO messages (team, seq, from_id, name, text, to_json, ts) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		team, seq, msg.From, msg.Name, msg.Text, string(to), msg.Time.UnixNano(),
	); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

// after returns a team's messages numbered above seq, oldest first.
func (s *store) after(team string, seq int) ([]Message, error) {
	// Reading never creates the database: a board nobody has written to has
	// nothing to load, and not opening the file leaves it free to be removed.
	if !s.exists() {
		return nil, nil
	}
	db, err := s.handle()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT seq, from_id, name, text, to_json, ts FROM messages WHERE team = ? AND seq > ? ORDER BY seq`,
		team, seq,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var (
			msg    Message
			to     string
			nanos  int64
			scanTo error
		)
		if err := rows.Scan(&msg.Seq, &msg.From, &msg.Name, &msg.Text, &to, &nanos); err != nil {
			return nil, err
		}
		if scanTo = json.Unmarshal([]byte(to), &msg.To); scanTo != nil {
			msg.To = Mentions(msg.Text)
		}
		msg.Time = time.Unix(0, nanos)
		out = append(out, msg)
	}
	return out, rows.Err()
}

// exists reports whether the database file is there or already open.
func (s *store) exists() bool {
	s.mu.Lock()
	open := s.db != nil
	s.mu.Unlock()
	if open {
		return true
	}
	_, err := os.Stat(filepath.Join(s.dir, storeFile))
	return err == nil
}

// close releases the database.
func (s *store) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idle != nil {
		s.idle.Stop()
	}
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}
