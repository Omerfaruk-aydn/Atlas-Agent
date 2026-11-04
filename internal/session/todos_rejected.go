package session

import "errors"

// ErrTodosRejected marks a task list update refused by a plan or quality
// rule (as opposed to a storage failure). The caller can show the reason to
// the model so it revises the plan instead of aborting the run.
var ErrTodosRejected = errors.New("task list update rejected")

type rejectedError struct{ err error }

func (e rejectedError) Error() string { return e.err.Error() }

func (e rejectedError) Unwrap() error { return e.err }

func (e rejectedError) Is(target error) bool { return target == ErrTodosRejected }

func rejected(err error) error { return rejectedError{err} }
