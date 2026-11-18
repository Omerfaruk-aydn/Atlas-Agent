package backend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

type fakeReviewer struct {
	run func(ctx context.Context, sessionID string, in agent.ReviewInput) (agent.ReviewOutput, error)
}

func (f fakeReviewer) ReviewChanges(ctx context.Context, sessionID string, in agent.ReviewInput) (agent.ReviewOutput, error) {
	return f.run(ctx, sessionID, in)
}

func waitState(t *testing.T, rb *reviewBook, id string) proto.Review {
	t.Helper()
	var got proto.Review
	require.Eventually(t, func() bool {
		r, err := rb.get(id)
		require.NoError(t, err)
		got = r
		return r.State != proto.ReviewRunning
	}, 5*time.Second, 5*time.Millisecond)
	return got
}

func TestReviewJobFinishesWithFindings(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	rv := fakeReviewer{run: func(_ context.Context, sid string, in agent.ReviewInput) (agent.ReviewOutput, error) {
		require.Equal(t, "s1", sid)
		require.Equal(t, []string{"a.go"}, in.Paths)
		return agent.ReviewOutput{Summary: "ok", Structured: true, Model: "m", Findings: []agent.ReviewFinding{{Severity: "high", Title: "Bug", File: "a.go", Line: 3}}}, nil
	}}
	started, err := rb.start(t.Context(), "s1", proto.ReviewRequest{Task: "t", Paths: []string{"a.go"}}, rv)
	require.NoError(t, err)
	require.Equal(t, proto.ReviewRunning, started.State)
	require.NotNil(t, started.Findings, "a running review has an empty list, not null")

	done := waitState(t, &rb, started.ID)
	require.Equal(t, proto.ReviewDone, done.State)
	require.True(t, done.Structured)
	require.Equal(t, "m", done.Model)
	require.Equal(t, []proto.ReviewFinding{{Severity: "high", Title: "Bug", File: "a.go", Line: 3}}, done.Findings)
	require.NotZero(t, done.FinishedAt)
}

func TestReviewJobReportsAFailureAndSurvivesAPanic(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	failed, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, fakeReviewer{run: func(context.Context, string, agent.ReviewInput) (agent.ReviewOutput, error) {
		return agent.ReviewOutput{}, errors.New("provider down")
	}})
	require.NoError(t, err)
	got := waitState(t, &rb, failed.ID)
	require.Equal(t, proto.ReviewFailed, got.State)
	require.Equal(t, "provider down", got.Error)
	require.Empty(t, got.Findings)

	crashed, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, fakeReviewer{run: func(context.Context, string, agent.ReviewInput) (agent.ReviewOutput, error) {
		panic("boom")
	}})
	require.NoError(t, err)
	got = waitState(t, &rb, crashed.ID)
	require.Equal(t, proto.ReviewFailed, got.State)
	require.Contains(t, got.Error, "boom")
}

func TestReviewCancelStopsTheRunAndSticks(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	entered := make(chan struct{})
	rv := fakeReviewer{run: func(ctx context.Context, _ string, _ agent.ReviewInput) (agent.ReviewOutput, error) {
		close(entered)
		<-ctx.Done()
		return agent.ReviewOutput{}, ctx.Err()
	}}
	started, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
	require.NoError(t, err)
	<-entered

	stopped, err := rb.stop(started.ID)
	require.NoError(t, err)
	require.Equal(t, proto.ReviewCanceled, stopped.State)

	// The run ends with a context error; the review stays cancelled, with no error text.
	time.Sleep(50 * time.Millisecond)
	after, err := rb.get(started.ID)
	require.NoError(t, err)
	require.Equal(t, proto.ReviewCanceled, after.State)
	require.Empty(t, after.Error)

	again, err := rb.stop(started.ID)
	require.NoError(t, err)
	require.Equal(t, proto.ReviewCanceled, again.State)
	_, err = rb.stop("missing")
	require.ErrorIs(t, err, ErrReviewNotFound)
}

func TestReviewJobsAreLimitedAndTheLimitFrees(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	release := make(chan struct{})
	rv := fakeReviewer{run: func(context.Context, string, agent.ReviewInput) (agent.ReviewOutput, error) {
		<-release
		return agent.ReviewOutput{Structured: true}, nil
	}}
	var ids []string
	for i := 0; i < maxRunningReviews; i++ {
		r, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
		require.NoError(t, err)
		ids = append(ids, r.ID)
	}
	_, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
	require.ErrorIs(t, err, ErrReviewBusy)

	close(release)
	for _, id := range ids {
		waitState(t, &rb, id)
	}
	_, err = rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
	require.NoError(t, err, "finished reviews do not count against the limit")
}

// A cancelled review whose model call has not returned yet still occupies
// its place, so cancelling and starting again cannot pile up reviewers.
func TestACancelledReviewKeepsItsPlaceUntilItsRunReturns(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	release := make(chan struct{})
	entered := make(chan struct{}, maxRunningReviews+1)
	rv := fakeReviewer{run: func(context.Context, string, agent.ReviewInput) (agent.ReviewOutput, error) {
		entered <- struct{}{}
		<-release // Ignores the cancellation, as a slow provider call can.
		return agent.ReviewOutput{}, context.Canceled
	}}
	var ids []string
	for i := 0; i < maxRunningReviews; i++ {
		r, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
		require.NoError(t, err)
		ids = append(ids, r.ID)
		<-entered
	}
	for _, id := range ids {
		_, err := rb.stop(id)
		require.NoError(t, err)
	}
	_, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
	require.ErrorIs(t, err, ErrReviewBusy, "the cancelled runs have not returned")

	close(release)
	require.Eventually(t, func() bool {
		_, err := rb.start(t.Context(), "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "the places free once the runs return")
}

func TestReviewJobEndsWithItsWorkspace(t *testing.T) {
	t.Parallel()
	var rb reviewBook
	parent, cancel := context.WithCancel(t.Context())
	rv := fakeReviewer{run: func(ctx context.Context, _ string, _ agent.ReviewInput) (agent.ReviewOutput, error) {
		<-ctx.Done()
		return agent.ReviewOutput{}, ctx.Err()
	}}
	started, err := rb.start(parent, "s", proto.ReviewRequest{Paths: []string{"a.go"}}, rv)
	require.NoError(t, err)
	cancel()
	got := waitState(t, &rb, started.ID)
	require.Equal(t, proto.ReviewFailed, got.State)
}

func TestFinishedReviewsAreForgottenAfterAWhileAndCapped(t *testing.T) {
	t.Parallel()
	rb := reviewBook{jobs: map[string]*reviewJob{}}
	now := time.Now()
	rb.jobs["old"] = &reviewJob{exited: true, review: proto.Review{ID: "old", State: proto.ReviewDone, FinishedAt: now.Add(-2 * keepReviewFor).UnixMilli()}}
	rb.jobs["running"] = &reviewJob{review: proto.Review{ID: "running", State: proto.ReviewRunning}}
	for i := 0; i < maxKeptReviews+5; i++ {
		id := "f" + time.Duration(i).String()
		rb.jobs[id] = &reviewJob{exited: true, review: proto.Review{ID: id, State: proto.ReviewDone, FinishedAt: now.Add(-time.Duration(i) * time.Second).UnixMilli()}}
	}
	rb.pruneLocked(now)
	_, hasOld := rb.jobs["old"]
	require.False(t, hasOld)
	require.Contains(t, rb.jobs, "running")
	finished := 0
	for _, j := range rb.jobs {
		if j.review.State != proto.ReviewRunning {
			finished++
		}
	}
	require.Equal(t, maxKeptReviews, finished)
	require.NotContains(t, rb.jobs, "f"+time.Duration(maxKeptReviews+4).String(), "the oldest finished review goes first")
}
