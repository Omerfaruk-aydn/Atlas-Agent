package engineering

import "errors"

// ErrOperationRefused marks a tool call that Begin declined on purpose: a
// pause, an exhausted budget or a call that keeps failing. Any other Begin
// error is a failure to read or write execution state.
var ErrOperationRefused = errors.New("operation refused")

type refusedError struct{ err error }

func (e refusedError) Error() string { return e.err.Error() }

func (e refusedError) Unwrap() error { return e.err }

func (e refusedError) Is(target error) bool { return target == ErrOperationRefused }

func refused(err error) error { return refusedError{err} }
