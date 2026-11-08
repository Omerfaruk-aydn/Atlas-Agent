// Package testenv makes tests independent of the machine they run on.
package testenv

import (
	"os"
	"path/filepath"
)

// KeepGitInsideTemp stops git from searching above the temporary directory's
// parent for a repository. Tests that make a directory with t.TempDir() and
// expect it not to be a repository would otherwise find the repository a
// developer keeps around their user folder, and fail only on their machine.
// Call it from TestMain before the tests run.
func KeepGitInsideTemp() {
	_ = os.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(os.TempDir()))
}
