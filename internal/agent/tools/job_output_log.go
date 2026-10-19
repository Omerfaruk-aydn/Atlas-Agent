package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A long job is read many times while it runs: a test suite, a build, a
// server. Returning everything it has printed on every read puts the same
// lines into the context again and again. So the full output is kept in a
// log under .atlas/logs, rewritten on each read, and a read returns only
// what the job printed since the previous one, with a note saying how much
// came before and where it is. Nothing is lost: the earlier output was
// already returned and the log holds all of it. When the log cannot be
// written, a read returns the whole output as before.

// jobCursor is how much of a job's output earlier reads returned. Cursors
// are kept per session: a sub-agent reading the same job has a context of
// its own, and its reads say nothing about what the parent has seen.
type jobCursor struct {
	stdout, stderr int
}

var (
	jobCursorsMu sync.Mutex
	jobCursors   = map[string]jobCursor{}
)

// jobOutputView is what one read of a job returns.
type jobOutputView struct {
	stdout, stderr string
	// note says what earlier reads returned and where the full output is;
	// empty on a first read.
	note string
}

// readJobOutput returns the output of job id since the previous read in
// session and advances the cursor. root is the project directory; without
// one, or when the log cannot be written, the whole output is returned.
func readJobOutput(root, session, id string, started time.Time, stdout, stderr string, done bool) jobOutputView {
	key := session + "\x00" + id
	jobCursorsMu.Lock()
	cursor := jobCursors[key]
	if done {
		// A finished job is not read for new output again; a later read
		// returns everything once more.
		delete(jobCursors, key)
	} else {
		jobCursors[key] = jobCursor{stdout: len(stdout), stderr: len(stderr)}
	}
	jobCursorsMu.Unlock()

	full := jobOutputView{stdout: stdout, stderr: stderr}
	// Output only grows; a shorter one means the buffer was replaced, so
	// it is shown whole.
	if root == "" || (cursor.stdout == 0 && cursor.stderr == 0) || cursor.stdout > len(stdout) || cursor.stderr > len(stderr) {
		return full
	}
	rel, err := writeJobLog(root, id, started, stdout, stderr)
	if err != nil {
		return full
	}
	earlier := strings.Count(stdout[:cursor.stdout], "\n") + strings.Count(stderr[:cursor.stderr], "\n")
	view := jobOutputView{stdout: stdout[cursor.stdout:], stderr: stderr[cursor.stderr:]}
	view.note = fmt.Sprintf("[Output since your last read of this job. The %d lines before it were returned earlier; the full output is in %s.]", earlier, rel)
	if view.stdout == "" && view.stderr == "" {
		view.note = fmt.Sprintf("[No new output since your last read of this job. The full output (%d lines) is in %s.]", earlier, rel)
	}
	return view
}

// writeJobLog saves the full output of job id, replacing the previous copy,
// and returns its path relative to root. The name starts with the job's
// start time, so the logs are pruned with the command logs, oldest first.
func writeJobLog(root, id string, started time.Time, stdout, stderr string) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(spillLogDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("cmd-%s-job-%s.log", started.Format("20060102-150405"), safeJobID(id))
	content := stdout
	if stderr != "" {
		content += "\n--- stderr ---\n" + stderr
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	pruneSpills(dir)
	return spillLogDir + "/" + name, nil
}

func safeJobID(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
			return r
		}
		return '_'
	}, id)
}
