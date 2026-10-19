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

