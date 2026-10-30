package workplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

// Two sessions can work in the same project at once. The plan the agent
// writes and the facts about the project (a flaky command, a green
// checkpoint) belong to the project and stay shared. Atlas's own progress
// through that plan (what was reviewed, which features were followed up,
// what each turn cost) belongs to one session and is kept under
// .atlas/sessions/<id>/, so one session cannot overwrite another's.
// Everything Atlas writes goes through an atomic replace and, where several
// writers may read-modify-write the same file, a kernel file lock.

// SessionsDir holds the per-session bookkeeping.
const SessionsDir = "sessions"

// lockWait bounds how long an update waits for another writer.
const lockWait = 10 * time.Second

// sessionDir returns the directory for a session's bookkeeping, or the plan
// directory itself for the empty (legacy, project-wide) session.
func sessionDir(root, session string) string {
	if session == "" {
		return filepath.Join(root, Dir)
	}
	return filepath.Join(root, Dir, SessionsDir, safeSession(session))
}

// safeSession keeps a session id usable as a single path element.
func safeSession(session string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, session)
	if clean != session || len(clean) > 64 {
		return clean[:min(len(clean), 40)] + "-" + Hash(session)
	}
	return clean
}

// writeFileAtomic replaces path with data through a temporary file in the
// same directory, so a reader or a crash never sees a half-written file.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		cleanup()
		return err
	}
	if err := renameReplace(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// renameReplace moves src over dst. On Windows a rename onto a file that a
// reader has open fails for as long as the reader holds it; readers here
// read a small file whole, so a brief retry gets past them.
func renameReplace(src, dst string) error {
	var err error
	for attempt := range 8 {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}

// withFileLock runs fn while holding an exclusive lock for path. It uses the
// kernel's advisory file lock (see package lock), so it excludes other Atlas
// processes as well as other goroutines, and a holder that crashes releases it
// at once: there is no stale lock to detect or break. Waiting is bounded: a
// holder that never lets go is an error, not a hang.
func withFileLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("lock %s: %w", filepath.Base(path), err)
	}
	defer release()
	return fn()
}

// sessionState is the part of State that belongs to one session; Flaky is
// project knowledge and lives in the shared state file.
func sessionState(s State) State {
	s.Flaky = nil
	return s
}

// LoadSessionState reads a session's state together with the project's
// flaky commands. A session that has none yet starts from the project-wide
// file, so existing work is not forgotten when sessions are introduced.
func LoadSessionState(root, session string) State {
	shared := LoadState(root)
	if session == "" {
		return shared
	}
	data, err := os.ReadFile(filepath.Join(sessionDir(root, session), StateFile))
	if err != nil {
		shared.Flaky = slices.Clone(shared.Flaky)
		return shared
	}
	var s State
	if json.Unmarshal(data, &s) != nil {
		s = State{}
	}
	s.Flaky = shared.Flaky
	return s
}

// UpdateState applies fn to the session's state and saves it. The session's
// progress goes to its own file; flaky commands are merged into the shared
// file under a lock, so concurrent sessions each add theirs without losing
// the other's.
func UpdateState(root, session string, fn func(*State) error) error {
	if session == "" {
		return withFileLock(filepath.Join(root, Dir, StateFile), func() error {
			s := LoadState(root)
			if err := fn(&s); err != nil {
				return err
			}
			return saveStateFile(filepath.Join(root, Dir, StateFile), s)
		})
	}
	sharedPath := filepath.Join(root, Dir, StateFile)
	ownPath := filepath.Join(sessionDir(root, session), StateFile)
	return withFileLock(ownPath, func() error {
		s := LoadSessionState(root, session)
		before := slices.Clone(s.Flaky)
		if err := fn(&s); err != nil {
			return err
		}
		if err := saveStateFile(ownPath, sessionState(s)); err != nil {
			return err
		}
		if slices.Equal(before, s.Flaky) {
			return nil
		}
		return withFileLock(sharedPath, func() error {
			shared := LoadState(root)
			for _, command := range s.Flaky {
				shared.AddFlaky(command)
			}
			return saveStateFile(sharedPath, shared)
		})
	})
}

func saveStateFile(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

// loadCostsAt reads one cost record; a missing or corrupt file is empty.
func loadCostsAt(path string) (Costs, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Costs{}, nil
	}
	if err != nil {
		return nil, err
	}
	costs := Costs{}
	if json.Unmarshal(data, &costs) != nil {
		return Costs{}, nil // A corrupt record restarts the count.
	}
	return costs, nil
}

// sessionCosts sums the legacy project-wide record and every session's, so
// the plan summary shows what the feature has cost across sessions.
func sessionCosts(root string) (Costs, error) {
	total, err := loadCostsAt(filepath.Join(root, Dir, MetricsFile))
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, Dir, SessionsDir))
	if err != nil {
		return total, nil //nolint:nilerr // No sessions yet.
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		costs, err := loadCostsAt(filepath.Join(root, Dir, SessionsDir, entry.Name(), MetricsFile))
		if err != nil {
			return nil, err
		}
		for id, c := range costs {
			t := total[id]
			t.Turns += c.Turns
			t.Millis += c.Millis
			t.Tokens += c.Tokens
			total[id] = t
		}
	}
	return total, nil
}
