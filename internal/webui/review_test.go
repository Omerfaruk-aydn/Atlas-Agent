package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

func waitReview(t *testing.T, b *browser, wsID, id string, timeout time.Duration) proto.Review {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var review proto.Review
		require.Equal(t, http.StatusOK, b.json(http.MethodGet, fmt.Sprintf("/api/workspaces/%s/reviews/%s", wsID, id), nil, &review))
		if review.State != proto.ReviewRunning {
			return review
		}
		require.True(t, time.Now().Before(deadline), "the review did not finish in %s", timeout)
		time.Sleep(100 * time.Millisecond)
	}
}

// toolNames lists the tools a recorded model request offered.
func toolNames(req map[string]any) []string {
	var names []string
	tools, _ := req["tools"].([]any)
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		if name, ok := fn["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// A review is done by a read-only agent that starts empty: it sees the task
// and the files, never the chat, it cannot be handed a writing tool, it
// leaves no session behind, and its findings come back ordered and typed.
func TestReviewIsReadOnlyIndependentAndLeavesNoTrace(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)
	b.prompt(ws.ID, sess.ID, "a private story about why the code is right")
	h.waitIdle(ws.ID, sess.ID, 30*time.Second)
	// The answer is stored a moment after the run reports idle.
	require.Eventually(t, func() bool { return len(b.messages(ws.ID, sess.ID)) >= 2 }, 10*time.Second, 50*time.Millisecond)
	before := len(b.messages(ws.ID, sess.ID))

	var started proto.Review
	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/reviews"), proto.ReviewRequest{Task: "add a feature", Paths: []string{"main.go", "util.go"}}, &started))
	require.Equal(t, proto.ReviewRunning, started.State)
	require.NotEmpty(t, started.ID)

	review := waitReview(t, b, ws.ID, started.ID, 60*time.Second)
	require.Equal(t, proto.ReviewDone, review.State, review.Error)
	require.True(t, review.Structured)
	require.Equal(t, "One problem found.", review.Summary)
	require.Len(t, review.Findings, 2)
	require.Equal(t, "high", review.Findings[0].Severity, "worst first")
	require.Equal(t, "Empty input panics", review.Findings[0].Title)
	require.Equal(t, "main.go", review.Findings[0].File)
	require.Equal(t, 7, review.Findings[0].Line)
	require.Equal(t, "info", review.Findings[1].Severity, "a nit is the lowest level")

	var reviewRequest map[string]any
	for _, req := range h.llm.Requests() {
		raw, _ := json.Marshal(req["messages"])
		// The request that carries tools is the reviewer's own; the title
		// call that may follow carries the same text without any.
		if strings.Contains(string(raw), "changed_files") && len(toolNames(req)) > 0 {
			reviewRequest = req
		}
	}
	require.NotNil(t, reviewRequest, "the reviewer made a model request")
	raw, _ := json.Marshal(reviewRequest["messages"])
	require.NotContains(t, string(raw), "private story", "the reviewer must not see the chat")
	require.Contains(t, string(raw), "add a feature")
	names := toolNames(reviewRequest)
	require.NotEmpty(t, names)
	for _, forbidden := range []string{"bash", "edit", "multiedit", "write", "download", "fetch", "agent", "delegate"} {
		require.NotContains(t, names, forbidden, "the reviewer was offered %q", forbidden)
	}

	require.Len(t, b.messages(ws.ID, sess.ID), before, "nothing is added to the reviewed chat")
	var sessions []proto.Session
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, "/api/workspaces/"+ws.ID+"/sessions?side=1", nil, &sessions))
	for _, s := range sessions {
		require.NotEqual(t, "Review", s.Title, "the scratch session is deleted")
	}
}

func TestReviewRequestsAreChecked(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)
	path := wsPath(ws.ID, "/sessions/", sess.ID, "/reviews")

	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, path, proto.ReviewRequest{}, nil), "no files")
	many := make([]string, maxReviewPaths+1)
	for i := range many {
		many[i] = fmt.Sprintf("f%d.go", i)
	}
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, path, proto.ReviewRequest{Paths: many}, nil), "too many files")
	require.Equal(t, http.StatusBadRequest, b.json(http.MethodPost, path, proto.ReviewRequest{Paths: []string{"  "}}, nil), "a blank path")
	require.Equal(t, http.StatusNotFound, b.json(http.MethodGet, "/api/workspaces/"+ws.ID+"/reviews/nope", nil, nil))
	require.Equal(t, http.StatusNotFound, b.json(http.MethodDelete, "/api/workspaces/"+ws.ID+"/reviews/nope", nil, nil))
}

func TestARunningReviewCanBeCancelled(t *testing.T) {
	h := newHarness(t)
	b := h.signIn()
	ws := b.open(h.project)
	sess := b.newSession(ws.ID)

	var started proto.Review
	require.Equal(t, http.StatusAccepted, b.json(http.MethodPost, wsPath(ws.ID, "/sessions/", sess.ID, "/reviews"), proto.ReviewRequest{Task: "[[review-slow]]", Paths: []string{"main.go"}}, &started))
	// Give the reviewer time to start its slow answer.
	time.Sleep(500 * time.Millisecond)

	var stopped proto.Review
	require.Equal(t, http.StatusOK, b.json(http.MethodDelete, "/api/workspaces/"+ws.ID+"/reviews/"+started.ID, nil, &stopped))
	require.Equal(t, proto.ReviewCanceled, stopped.State)
	require.NotZero(t, stopped.FinishedAt)

	// It stays cancelled when the run ends, and cancelling again changes nothing.
	time.Sleep(500 * time.Millisecond)
	var again proto.Review
	require.Equal(t, http.StatusOK, b.json(http.MethodGet, "/api/workspaces/"+ws.ID+"/reviews/"+started.ID, nil, &again))
	require.Equal(t, proto.ReviewCanceled, again.State)
	require.Empty(t, again.Error)
}
