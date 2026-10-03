package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// withFastPolling shrinks the poll/backoff delays so a test that drives
// several onboardUser rounds doesn't actually wait tens of seconds, and
// restores them afterward so other tests (and any future ones added to
// this package) see the real production values.
func withFastPolling(t *testing.T) {
	t.Helper()
	prevPoll, prevBackoff := pollDelay, rateLimitBackoff
	pollDelay = time.Millisecond
	rateLimitBackoff = time.Millisecond
	t.Cleanup(func() { pollDelay, rateLimitBackoff = prevPoll, prevBackoff })
}

// pointCodeAssistAt redirects postCodeAssist's requests to srv for the
// duration of the test.
func pointCodeAssistAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := codeAssistEndpoint
	codeAssistEndpoint = srv.URL
	t.Cleanup(func() { codeAssistEndpoint = prev })
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// A 429 from onboardUser used to abort discoverProject outright, on the
// theory that any non-200 response was a hard failure. In practice it
// is Google's onboarding backend asking the client to slow down, not a
// rejection of the account -- a real login was observed failing this
// way after seventeen clean "not done yet" polls, with attempts still
// left in its budget. This pins the fix: the poll continues past 429s
// and still succeeds once the backend stops rate-limiting it.
func TestDiscoverProjectRetriesPast429AndSucceeds(t *testing.T) {
	withFastPolling(t)

	var onboardCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "loadCodeAssist"):
			writeJSON(w, loadCodeAssistResponse{AllowedTiers: []tierInfo{{ID: "free"}}})
		case strings.HasSuffix(r.URL.Path, "onboardUser"):
			n := onboardCalls.Add(1)
			if n <= 2 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`))
				return
			}
			resp := onboardUserResponse{Done: true}
			resp.Response.CloudaicompanionProject.ID = "proj-123"
			writeJSON(w, resp)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)

	var reports []string
	project, tier, err := discoverProject(t.Context(), "token", func(s string) { reports = append(reports, s) })

	require.NoError(t, err)
	require.Equal(t, "proj-123", project)
	require.Equal(t, "free", tier)
	require.Equal(t, int32(3), onboardCalls.Load(), "two rate-limited attempts, then the successful third")
	require.True(t, containsSubstring(reports, "rate-limited"), "the rate-limit rounds must be reported distinctly, not silently retried")
}

// If onboardUser is rate-limited on every attempt, discoverProject must
// give up well before ctx expires rather than run out the clock --
// google-gemini/gemini-cli's own issue tracker has a report of a client
// that retried a 429 unconditionally "hanging indefinitely without
// surfacing error" for exactly this case, since a 429 this persistent
// tends to be an account-level quota problem retrying cannot fix. The
// error must say so distinctly, not just that it timed out.
func TestDiscoverProjectGivesUpAfterPersistent429sWithoutWaitingForCtx(t *testing.T) {
	withFastPolling(t)

	var onboardCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "loadCodeAssist"):
			writeJSON(w, loadCodeAssistResponse{AllowedTiers: []tierInfo{{ID: "free"}}})
		case strings.HasSuffix(r.URL.Path, "onboardUser"):
			onboardCalls.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`))
		}
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)

	// Generous relative to how fast the persistent-rate-limit path
	// should give up (a handful of near-instant retries under
	// withFastPolling): if this fires first, the give-up path failed to
	// trigger and the test below would otherwise hang until it did.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, _, err := discoverProject(ctx, "token", nil)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "timed out", "a persistent rate limit is a distinct outcome from an ordinary provisioning timeout")
	require.Contains(t, err.Error(), "rate-limiting")
	require.Contains(t, err.Error(), "persistent")
	require.EqualValues(t, 7, onboardCalls.Load(), "gives up after maxConsecutiveRateLimits (6) retries, i.e. the 7th attempt")
}

// Interleaved 429s (a couple, then progress, then a couple more) must
// not trip the persistent-rate-limit give-up path: it counts consecutive
// 429s, resetting on any clean response, so a backend that is merely
// flaky rather than exhausted still gets to finish onboarding.
func TestDiscoverProjectResetsConsecutiveCountOnACleanResponse(t *testing.T) {
	withFastPolling(t)

	var onboardCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "loadCodeAssist"):
			writeJSON(w, loadCodeAssistResponse{AllowedTiers: []tierInfo{{ID: "free"}}})
		case strings.HasSuffix(r.URL.Path, "onboardUser"):
			n := onboardCalls.Add(1)
			// 429, 429, not-done, 429, 429, not-done, then done -- twice
			// as many consecutive 429s total (4) as any single run (2)
			// ever reaches, which would trip maxConsecutiveRateLimits
			// (6) if the count were not reset between runs.
			switch n {
			case 1, 2, 4, 5:
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`))
			case 7:
				resp := onboardUserResponse{Done: true}
				resp.Response.CloudaicompanionProject.ID = "proj-456"
				writeJSON(w, resp)
			default:
				writeJSON(w, onboardUserResponse{Done: false})
			}
		}
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)

	project, _, err := discoverProject(t.Context(), "token", nil)

	require.NoError(t, err)
	require.Equal(t, "proj-456", project)
	require.EqualValues(t, 7, onboardCalls.Load())
}

// A non-429 failure (a real rejection, not a rate limit) must still
// abort immediately rather than burning through the whole poll budget.
func TestDiscoverProjectStillFailsFastOnANonRateLimitError(t *testing.T) {
	withFastPolling(t)

	var onboardCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "loadCodeAssist"):
			writeJSON(w, loadCodeAssistResponse{AllowedTiers: []tierInfo{{ID: "free"}}})
		case strings.HasSuffix(r.URL.Path, "onboardUser"):
			onboardCalls.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"status":"PERMISSION_DENIED"}}`))
		}
	}))
	defer srv.Close()
	pointCodeAssistAt(t, srv)

	_, _, err := discoverProject(t.Context(), "token", nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "onboardUser")
	require.Equal(t, int32(1), onboardCalls.Load(), "a non-rate-limit error must abort on the first attempt")
}

func containsSubstring(reports []string, substr string) bool {
	for _, r := range reports {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}
