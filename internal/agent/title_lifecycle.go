package agent

import (
	"context"
	"errors"
)

var errTitleShutdown = errors.New("title generation stopped during shutdown")

type titleJob struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

// startTitle detaches title generation from normal turn completion while
// retaining cancellation and completion tracking for application shutdown.
func (a *sessionAgent) startTitle(ctx context.Context, sessionID, prompt string) {
	a.titleMu.Lock()
	defer a.titleMu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if _, running := a.titleJobs[sessionID]; running {
		return
	}
	if a.titleJobs == nil {
		a.titleJobs = make(map[string]titleJob)
	}
	titleCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	job := titleJob{cancel: cancel, done: make(chan struct{})}
	a.titleJobs[sessionID] = job
	go func() {
		defer func() {
			cancel(nil)
			a.titleMu.Lock()
			delete(a.titleJobs, sessionID)
			close(job.done)
			a.titleMu.Unlock()
		}()
		a.GenerateTitle(titleCtx, sessionID, prompt)
	}()
}

func (a *sessionAgent) cancelTitles() []<-chan struct{} {
	a.titleMu.Lock()
	defer a.titleMu.Unlock()
	done := make([]<-chan struct{}, 0, len(a.titleJobs))
	for _, job := range a.titleJobs {
		job.cancel(errTitleShutdown)
		done = append(done, job.done)
	}
	return done
}
