package webui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/proto"
)

// Event stream contract between the gateway and browser tabs.
//
// The Atlas server's SSE stream carries no event IDs, so a browser that
// reconnects could neither resume nor tell that it missed something. The
// hub numbers every event it relays with a per-gateway sequence, keeps the
// most recent ones in a ring, and replays from Last-Event-ID. When a tab
// asks for events older than the ring, or the hub itself lost upstream
// events, it sends a "resync" event and the tab reloads its snapshots.
//
// Synthetic event types the hub adds to the server's own:
//
//   - hello:              first event of every stream; carries the hub's
//     instance ID and the last sequence number.
//   - resync:             the tab's view may have a gap; reload snapshots.
//   - session_status:     a session's run state as the server reports it
//     (busy and queued prompt count), emitted on change.
//   - connection:         the hub's upstream stream went offline or
//     came back.
//   - workspace_replaced: the server lost the workspace (e.g. it
//     restarted) and the gateway opened it again under a new ID.
const (
	ringSize          = 4096
	subscriberBuffer  = 1024
	statusDebounce    = 75 * time.Millisecond
	busyPollInterval  = 2 * time.Second
	maxUpstreamEvent  = 64 << 20
	upstreamRetryBase = 250 * time.Millisecond
	upstreamRetryMax  = 5 * time.Second
)

type hubEvent struct {
	Seq     uint64          `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type subscriber struct {
	ch   chan hubEvent
	done chan struct{}
	once sync.Once
}

func (s *subscriber) close() { s.once.Do(func() { close(s.done) }) }

type sessionState struct {
	busy    bool
	queued  int
	known   bool
	pending *time.Timer
	polling bool
}

// hub relays one workspace's events from the server to browser tabs.
type hub struct {
	g    *Gateway
	path string

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	id        string
	seq       uint64
	ring      []hubEvent
	subs      map[*subscriber]struct{}
	connected bool
	sessions  map[string]*sessionState
	firstConn chan struct{}
	firstOnce sync.Once
}

func newHub(g *Gateway, id, path string) *hub {
	ctx, cancel := context.WithCancel(g.ctx)
	return &hub{
		g:         g,
		id:        id,
		path:      path,
		ctx:       ctx,
		cancel:    cancel,
		subs:      make(map[*subscriber]struct{}),
		sessions:  make(map[string]*sessionState),
		firstConn: make(chan struct{}),
	}
}

func (h *hub) workspaceID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.id
}

// publish assigns the next sequence number, stores the event and fans it
// out. A tab too slow to keep up is disconnected rather than allowed to
// stall the others; it resumes from the ring when it reconnects.
func (h *hub) publish(typ string, payload any) {
	raw, ok := payload.(json.RawMessage)
	if !ok {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			h.g.logger.Error("Failed to encode web event", "type", typ, "error", err)
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	ev := hubEvent{Seq: h.seq, Type: typ, Payload: raw}
	if len(h.ring) == ringSize {
		copy(h.ring, h.ring[1:])
		h.ring = h.ring[:ringSize-1]
	}
	h.ring = append(h.ring, ev)
	for s := range h.subs {
		select {
		case s.ch <- ev:
		default:
			delete(h.subs, s)
			s.close()
		}
	}
}

// subscribe registers a tab. after is the last sequence the tab saw (0 for
// none). It returns the events to replay first and whether the tab must
// resync because the ring no longer covers what it missed.
func (h *hub) subscribe(after uint64) (*subscriber, []hubEvent, bool, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &subscriber{ch: make(chan hubEvent, subscriberBuffer), done: make(chan struct{})}
	h.subs[s] = struct{}{}
	if after == 0 {
		return s, nil, false, h.seq
	}
	if after > h.seq {
		// The tab saw sequence numbers this hub never issued: it was
		// talking to an earlier gateway process.
		return s, nil, true, h.seq
	}
	if after == h.seq {
		return s, nil, false, h.seq
	}
	if len(h.ring) == 0 || h.ring[0].Seq > after+1 {
		return s, nil, true, h.seq
	}
	var replay []hubEvent
	for _, ev := range h.ring {
		if ev.Seq > after {
			replay = append(replay, ev)
		}
	}
	return s, replay, false, h.seq
}

func (h *hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
	s.close()
}

// status is the hub's current view, for a tab's initial snapshot.
func (h *hub) status() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	sessions := map[string]map[string]any{}
	for id, st := range h.sessions {
		if st.known {
			sessions[id] = map[string]any{"busy": st.busy, "queued": st.queued}
		}
	}
	return map[string]any{"connected": h.connected, "seq": h.seq, "instance": h.g.instance, "sessions": sessions}
}

// run keeps the upstream stream open for the life of the hub. The open
// stream is also the gateway's claim on the workspace: while it is up the
// server keeps the workspace, and every run in it, alive regardless of how
// many browser tabs come and go.
func (h *hub) run() {
	backoff := upstreamRetryBase
	lostOnce := false
	for h.ctx.Err() == nil {
		err := h.stream(lostOnce)
		if h.ctx.Err() != nil {
			return
		}
		h.setConnected(false)
		lostOnce = true
		var gone errWorkspaceGone
		if errors.As(err, &gone) {
			if rerr := h.reopen(); rerr != nil {
				h.g.logger.Warn("Failed to reopen web workspace", "path", h.path, "error", rerr)
			} else {
				backoff = upstreamRetryBase
				continue
			}
		} else if err != nil {
			h.g.logger.Debug("Web workspace stream interrupted", "workspace", h.workspaceID(), "error", err)
			if h.g.opts.Reconnect != nil {
				if rerr := h.g.opts.Reconnect(h.ctx); rerr != nil {
					h.g.logger.Warn("Failed to reach Atlas server", "error", rerr)
				}
			}
		}
		select {
		case <-h.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, upstreamRetryMax)
	}
}

type errWorkspaceGone struct{ status int }

func (e errWorkspaceGone) Error() string { return fmt.Sprintf("workspace gone (status %d)", e.status) }

func (h *hub) setConnected(v bool) {
	h.mu.Lock()
	changed := h.connected != v
	h.connected = v
	h.mu.Unlock()
	if changed {
		state := "offline"
		if v {
			state = "online"
		}
		h.publish("connection", map[string]any{"state": state})
	}
}

// stream reads the server's SSE stream until it ends.
func (h *hub) stream(afterGap bool) error {
	id := h.workspaceID()
	q := url.Values{"client_id": []string{h.g.opts.Upstream.ClientID()}}
	rsp, err := h.g.opts.Upstream.Do(h.ctx, http.MethodGet, "/workspaces/"+url.PathEscape(id)+"/events", q, nil, http.Header{"Accept": []string{"text/event-stream"}})
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	if rsp.StatusCode == http.StatusNotFound {
		return errWorkspaceGone{status: rsp.StatusCode}
	}
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream: status %d", rsp.StatusCode)
	}
	h.setConnected(true)
	h.firstOnce.Do(func() { close(h.firstConn) })
	if afterGap {
		// Events published while the stream was down are lost for good;
		// every tab has to rebuild from snapshots.
		h.publish("resync", map[string]any{"reason": "upstream_reconnected"})
		h.refreshKnownSessions()
	}

	reader := bufio.NewReaderSize(rsp.Body, 64<<10)
	var data bytes.Buffer
	for {
		line, err := readLine(reader, maxUpstreamEvent)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch {
		case line == "":
			if data.Len() > 0 {
				h.relay(data.Bytes())
				data.Reset()
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
}

// readLine reads one line without its terminator, refusing lines longer
// than limit so a malformed stream cannot exhaust memory.
func readLine(r *bufio.Reader, limit int) (string, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return "", err
		}
		buf = append(buf, chunk...)
		if len(buf) > limit {
			return "", errors.New("event exceeds size limit")
		}
		if !isPrefix {
			return string(buf), nil
		}
	}
}

// upstreamEnvelope is the server's SSE payload: a discriminated union of
// pubsub events (see internal/pubsub.Payload).
type upstreamEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// relay forwards one server event and schedules a run-state check for
// the session it concerns.
func (h *hub) relay(data []byte) {
	var env upstreamEnvelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type == "" {
		h.g.logger.Debug("Dropping malformed upstream event", "error", err)
		return
	}
	if env.Type == "config_changed" {
		// The payload names only the workspace, but the change may have
		// touched credentials; the browser just needs to know to refetch.
		env.Payload = json.RawMessage(`{"type":"updated","payload":{}}`)
	}
	h.publish(env.Type, env.Payload)
	if sid := sessionIDOf(env); sid != "" {
		h.scheduleStatus(sid)
	}
}

// sessionIDOf extracts the session an upstream event concerns.
func sessionIDOf(env upstreamEnvelope) string {
	var inner struct {
		Payload struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
		} `json:"payload"`
	}
	if json.Unmarshal(env.Payload, &inner) != nil {
		return ""
	}
	switch env.Type {
	case "session":
		return inner.Payload.ID
	case "message", "run_complete", "agent_event", "permission_request", "question_batch_request":
		return inner.Payload.SessionID
	}
	return ""
}

// scheduleStatus debounces a run-state check for a session. Run state is
// always read back from the server rather than inferred from the event
// stream, so a late or duplicated event can never flip a finished session
// back to running.
func (h *hub) scheduleStatus(sid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.sessions[sid]
	if st == nil {
		st = &sessionState{}
		h.sessions[sid] = st
	}
	if st.pending != nil {
		return
	}
	st.pending = time.AfterFunc(statusDebounce, func() { h.checkStatus(sid) })
}

func (h *hub) checkStatus(sid string) {
	if h.ctx.Err() != nil {
		return
	}
	busy, queued, err := h.fetchStatus(sid)

	h.mu.Lock()
	st := h.sessions[sid]
	if st == nil {
		st = &sessionState{}
		h.sessions[sid] = st
	}
	st.pending = nil
	if err != nil {
		h.mu.Unlock()
		return
	}
	changed := !st.known || st.busy != busy || st.queued != queued
	st.busy, st.queued, st.known = busy, queued, true
	poll := busy && !st.polling
	if poll {
		st.polling = true
	}
	h.mu.Unlock()

	if changed {
		h.publish("session_status", map[string]any{"session_id": sid, "busy": busy, "queued": queued})
	}
	if poll {
		go h.pollWhileBusy(sid)
	}
}

// pollWhileBusy re-reads a busy session's state until it settles. The
// server's broker drops events for slow subscribers, so the event that
// ends a run is not guaranteed to arrive; polling bounds how long a tab
// can show a finished run as running.
func (h *hub) pollWhileBusy(sid string) {
	t := time.NewTicker(busyPollInterval)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
		}
		h.checkStatus(sid)
		h.mu.Lock()
		st := h.sessions[sid]
		stop := st == nil || !st.busy
		if stop && st != nil {
			st.polling = false
		}
		h.mu.Unlock()
		if stop {
			return
		}
	}
}

func (h *hub) refreshKnownSessions() {
	h.mu.Lock()
	ids := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.scheduleStatus(id)
	}
}

func (h *hub) fetchStatus(sid string) (bool, int, error) {
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	id := url.PathEscape(h.workspaceID())
	var sess proto.Session
	if err := h.g.getJSON(ctx, "/workspaces/"+id+"/sessions/"+url.PathEscape(sid), nil, &sess); err != nil {
		return false, 0, err
	}
	var queued int
	if err := h.g.getJSON(ctx, "/workspaces/"+id+"/agent/sessions/"+url.PathEscape(sid)+"/prompts/queued", nil, &queued); err != nil {
		queued = 0
	}
	return sess.IsBusy, queued, nil
}

// reopen re-creates a workspace the server no longer knows, under the
// same path, and points the hub at the new ID.
func (h *hub) reopen() error {
	ctx, cancel := context.WithTimeout(h.ctx, 2*time.Minute)
	defer cancel()
	ws, err := h.g.createUpstream(ctx, h.path)
	if err != nil {
		return err
	}
	h.mu.Lock()
	old := h.id
	h.id = ws.ID
	h.mu.Unlock()
	if old != ws.ID {
		h.g.rekey(old, ws.ID, h)
		h.publish("workspace_replaced", map[string]any{"old_id": old, "new_id": ws.ID})
	}
	return nil
}
