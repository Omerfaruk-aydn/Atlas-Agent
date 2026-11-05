//go:build !windows && !linux && !darwin

package procown

import (
	"errors"
	"runtime"
)

// CreationTime reports that this Unix system cannot identify a process
// instance. Ownership checks then fail closed.
func CreationTime(int) (int64, error) {
	return 0, errors.New("process creation time unavailable on " + runtime.GOOS)
}
