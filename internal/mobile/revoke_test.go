package mobile

import (
	"bufio"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// Two phones answering the same request at once: exactly one answer is
// applied, the other is told the request is no longer pending.
func TestConcurrentPermissionAnswersApplyOnce(t *testing.T) {
	t.Parallel()
	g, b := newGateway(t)
	first, second := newPhone(t, g), newPhone(t, g)
	first.pair(true)
	second.pair(true)
	req := permission.PermissionRequest{ID: "perm-race", SessionID: "s1", ToolCallID: "call-race", ToolName: "bash", Action: "execute"}
	b.mu.Lock()
	b.pending["perm-race"] = true
	b.mu.Unlock()
	g.Ingest(pubsub.Event[permission.PermissionRequest]{Payload: req})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i, p := range []*phone{first, second} {
		wg.Go(func() {
			r, _ := p.do("POST", "/api/permissions/perm-race", map[string]string{"decision": []string{"allow", "deny"}[i], "session_id": "s1", "tool_call_id": "call-race"}, map[string]string{"Idempotency-Key": "race"})
			codes <- r.StatusCode
		})
	}
	wg.Wait()
	close(codes)
	var got []int
	for c := range codes {
		got = append(got, c)
	}
	require.ElementsMatch(t, []int{http.StatusOK, http.StatusConflict}, got)
}

func TestRevokeAllEndsOpenStreamsAtOnceAndCancelsPendingPairings(t *testing.T) {
	t.Parallel()
	g, _ := newGateway(t)
	first, second := newPhone(t, g), newPhone(t, g)
	require.Equal(t, "accepted", first.pair(true))
	require.Equal(t, "accepted", second.pair(true))
	// A third phone is still waiting for desktop approval.
	u, _ := g.PairingURL()
	waiting := newPhone(t, g)
	_, out := waiting.do("POST", "/api/pair", map[string]string{"nonce": u[strings.Index(u, "#pair=")+len("#pair="):], "name": "Unknown"}, nil)
	pendingID := out["pending_id"].(string)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, first.base+"/api/events", nil)
	resp, err := first.client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	n, err := g.Auth.RevokeAll()
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Empty(t, g.Auth.Devices())
	_, err = g.Auth.Decide(pendingID, true)
	require.ErrorIs(t, err, ErrNotPending, "an undecided pairing cannot be accepted after revoking all")

	revoked := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if sc.Text() == "event: revoked" {
				revoked <- true
				return
			}
		}
		revoked <- false
	}()
	select {
	case ok := <-revoked:
		require.True(t, ok)
	case <-time.After(5 * time.Second):
		t.Fatal("a revoked phone kept its event stream")
	}
	r, _ := second.do("GET", "/api/me", nil, nil)
	require.Equal(t, http.StatusUnauthorized, r.StatusCode)
}
