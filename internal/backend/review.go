package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
	"github.com/google/uuid"
)

// A review takes minutes, longer than a request should be held open, so it
// runs as a job: starting one returns at once, and its state is read (and it
// may be cancelled) by id. Jobs belong to the workspace, live in memory and
// end with it.

var (
	// ErrReviewNotFound is returned for a review id the workspace does not know.
	ErrReviewNotFound = errors.New("review not found")
	// ErrReviewBusy is returned when the workspace already runs as many
	// reviews as it may.
	ErrReviewBusy = errors.New("too many reviews are running; wait for one to finish")
	// ErrReviewUnsupported is returned when the workspace's agent cannot review.
	ErrReviewUnsupported = errors.New("this agent cannot review changes")
	// ErrReviewEmpty is returned for a review with no file to look at.
	ErrReviewEmpty = errors.New("a review needs at least one file")
)

const (
	// maxRunningReviews bounds the reviews one workspace runs at once; each
	// is a model run that reads the project.
	maxRunningReviews = 3
	// maxKeptReviews bounds the finished reviews kept to be read back.
	maxKeptReviews = 50
	// keepReviewFor is how long a finished review stays readable.
	keepReviewFor = time.Hour
)

// reviewer is what a coordinator offers when it can review. It is asked for
// by type, not added to agent.Coordinator, so every coordinator stand-in does
// not have to grow a method it has no use for.
type reviewer interface {
	ReviewChanges(ctx context.Context, sessionID string, in agent.ReviewInput) (agent.ReviewOutput, error)
}

type reviewJob struct {
	review proto.Review
	cancel context.CancelFunc
	// exited is set when the goroutine has returned. A cancelled review
	// keeps its place until then, because the model call and the cleanup of
	// its scratch session may still be running.
	exited bool
}

// reviewBook holds a workspace's reviews. The zero value is ready to use.
type reviewBook struct {
	mu   sync.Mutex
	jobs map[string]*reviewJob
}

// StartReview begins reviewing the changes of a turn and returns the review
// in its running state. The caller reads the outcome with GetReview.
func (b *Backend) StartReview(workspaceID, sessionID string, req proto.ReviewRequest) (proto.Review, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Review{}, err
	}
	if ws.AgentCoordinator == nil {
		return proto.Review{}, ErrAgentNotInitialized
	}
	rv, ok := ws.AgentCoordinator.(reviewer)
	if !ok {
		return proto.Review{}, ErrReviewUnsupported
	}
	// Reject a request that could never run before a job exists for it.
	if len(req.Paths) == 0 {
		return proto.Review{}, ErrReviewEmpty
	}
	return ws.reviews.start(ws.ctx, sessionID, req, rv)
}

// GetReview returns a review by id.
func (b *Backend) GetReview(workspaceID, id string) (proto.Review, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Review{}, err
	}
	return ws.reviews.get(id)
}

// CancelReview stops a running review. A review that already finished is
// returned as it is.
func (b *Backend) CancelReview(workspaceID, id string) (proto.Review, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Review{}, err
	}
	return ws.reviews.stop(id)
}

func (rb *reviewBook) start(parent context.Context, sessionID string, req proto.ReviewRequest, rv reviewer) (proto.Review, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.pruneLocked(time.Now())
	running := 0
	for _, j := range rb.jobs {
		if !j.exited {
			running++
		}
	}
	if running >= maxRunningReviews {
		return proto.Review{}, ErrReviewBusy
	}
	if rb.jobs == nil {
		rb.jobs = map[string]*reviewJob{}
	}
	id := uuid.NewString()
	ctx, cancel := context.WithCancel(parent)
	job := &reviewJob{
		review: proto.Review{ID: id, SessionID: sessionID, State: proto.ReviewRunning, Findings: []proto.ReviewFinding{}, StartedAt: time.Now().UnixMilli()},
		cancel: cancel,
	}
	rb.jobs[id] = job
	go rb.run(ctx, job, sessionID, req, rv)
	return job.review, nil
}

// run does the review and records how it ended. Readers take copies of
// job.review under the lock.
func (rb *reviewBook) run(ctx context.Context, job *reviewJob, sessionID string, req proto.ReviewRequest, rv reviewer) {
	defer job.cancel()
	var out agent.ReviewOutput
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("the review crashed: %v", r)
				slog.Error("Review panicked", "session", sessionID, "panic", r)
			}
		}()
		out, err = rv.ReviewChanges(ctx, sessionID, agent.ReviewInput{Task: req.Task, Paths: req.Paths})
	}()

	rb.mu.Lock()
	defer rb.mu.Unlock()
	job.exited = true
	if job.review.State == proto.ReviewCanceled {
		// stop already recorded how it ended.
		return
	}
	job.review.FinishedAt = time.Now().UnixMilli()
	if err != nil {
		job.review.State = proto.ReviewFailed
		job.review.Error = err.Error()
		slog.Warn("Review failed", "session", sessionID, "error", err)
		return
	}
	job.review.State = proto.ReviewDone
	job.review.Summary = out.Summary
	job.review.Structured = out.Structured
	job.review.Model = out.Model
	job.review.Findings = make([]proto.ReviewFinding, 0, len(out.Findings))
	for _, f := range out.Findings {
		job.review.Findings = append(job.review.Findings, proto.ReviewFinding{
			Severity: f.Severity, Title: f.Title, File: f.File, Line: f.Line, Explanation: f.Explanation, Suggestion: f.Suggestion,
		})
	}
}

func (rb *reviewBook) get(id string) (proto.Review, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	job, ok := rb.jobs[id]
	if !ok {
		return proto.Review{}, ErrReviewNotFound
	}
	return job.review, nil
}

func (rb *reviewBook) stop(id string) (proto.Review, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	job, ok := rb.jobs[id]
	if !ok {
		return proto.Review{}, ErrReviewNotFound
	}
	if job.review.State == proto.ReviewRunning {
		job.review.State = proto.ReviewCanceled
		job.review.FinishedAt = time.Now().UnixMilli()
		job.cancel()
	}
	return job.review, nil
}

// pruneLocked drops reviews that finished long ago, and the oldest finished
// ones beyond the number kept.
func (rb *reviewBook) pruneLocked(now time.Time) {
	var finished []string
	for id, j := range rb.jobs {
		if j.review.State == proto.ReviewRunning || !j.exited {
			continue
		}
		if now.Sub(time.UnixMilli(j.review.FinishedAt)) > keepReviewFor {
			delete(rb.jobs, id)
			continue
		}
		finished = append(finished, id)
	}
	if len(finished) <= maxKeptReviews {
		return
	}
	sort.Slice(finished, func(i, j int) bool {
		return rb.jobs[finished[i]].review.FinishedAt < rb.jobs[finished[j]].review.FinishedAt
	})
	for _, id := range finished[:len(finished)-maxKeptReviews] {
		delete(rb.jobs, id)
	}
}
