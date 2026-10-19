package codemap

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Files that change in the same commits belong together even when no
// import says so: a component and its test, a handler and its route, a
// schema and its migration. The history is read once per commit at HEAD.

const (
	// historyCommits is how far back the history is read.
	historyCommits = 600
	// maxCommitFiles skips sweeping commits (a rename, a reformat) whose
	// files say nothing about each other.
	maxCommitFiles = 40
	// minTogether is the least number of shared commits that counts.
	minTogether = 2
)

// Related is a file that changed together with another.
type Related struct {
	Path string
	// Together is the number of commits that changed both files, and
	// Commits the number that changed the first one.
	Together, Commits int
}

type cochange struct {
	head    string
	commits map[string]int
	pairs   map[string]map[string]int
}

