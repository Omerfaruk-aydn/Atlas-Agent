// Package teams gives sub-agents spawned from the same top-level session a
// shared, in-memory mailbox: any of them (running in parallel or nested
// arbitrarily deep) can broadcast a note and any other member can read it,
// without the parent's agent tool call ever having to return first.
//
// This is deliberately not a redesign of how sub-agents run. The agent
// tool's Run call still blocks the parent until the sub-agent's turn
// finishes (see coordinator.runSubAgent) -- that turn-completion logic is
// left untouched. What this adds is a side channel: while several
// sub-agents are running concurrently under the same ParallelAgentTool
// step, they can coordinate through Send/Since/Wait instead of only ever
// reporting back through their own final text output.
package teams

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Message is one entry in a team's mailbox.
type Message struct {
	Seq int
	// From is the sender's session id; Name is its handle on the board.
	From string
	Name string
	Text string
	// To lists who the text addresses: member handles, "all", or "#channel".
	// It is read from the text (see Mentions), so a message with no @ is a
	// broadcast nobody is paged for.
	To   []string
	Time time.Time
}

// Registry tracks team membership and per-team message history. The zero
// value is not usable; construct with NewRegistry. Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	memberOf map[string]string
	messages map[string][]Message
	counter  map[string]int
	waiters  map[string]chan struct{}

	members   map[string]*member
	loaded    map[string]bool
	store     *store
	nextOrder int
	clock     func() time.Time
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		memberOf: make(map[string]string),
		messages: make(map[string][]Message),
		counter:  make(map[string]int),
		waiters:  make(map[string]chan struct{}),
		members:  make(map[string]*member),
		loaded:   make(map[string]bool),
		clock:    time.Now,
	}
}

// Join records childID as a member of parentID's team. If parentID is not
// yet a member of any team, it becomes the root of a new one. Nesting is
// transparent: a sub-agent that itself spawns sub-agents passes its own
// session ID as parentID, so every descendant ends up sharing the same
// team as the original top-level session. Returns the resulting team ID.
func (r *Registry) Join(parentID, childID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	team, ok := r.memberOf[parentID]
	if !ok {
		team = parentID
		r.memberOf[parentID] = team
	}
	r.memberOf[childID] = team
	return team
}

// TeamFor returns the team sessionID belongs to. A session not seen
// before is registered as the root of its own (so far solitary) team, so
// team_send/team_read work even for a top-level session that has not
// spawned any sub-agents yet.
func (r *Registry) TeamFor(sessionID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	team, ok := r.memberOf[sessionID]
	if !ok {
		team = sessionID
		r.memberOf[sessionID] = team
	}
	return team
}

// Send appends a message to teamID's mailbox and wakes any goroutine
// currently blocked in Wait for that team.
func (r *Registry) Send(teamID, from, text string) Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked(teamID)
	now := r.clock()
	msg := Message{From: from, Name: r.nameLocked(from), Text: text, To: Mentions(text), Time: now}
	if r.store != nil {
		// The database numbers the message, so another process posting at the
		// same moment cannot take the same number.
		if seq, err := r.store.insert(teamID, msg); err != nil {
			slog.Warn("Could not save a team board message; keeping it in memory", "error", err)
		} else {
			msg.Seq = seq
			r.loadLocked(teamID)
		}
	}
	if msg.Seq == 0 {
		r.counter[teamID]++
		msg.Seq = r.counter[teamID]
		r.messages[teamID] = append(r.messages[teamID], msg)
	}
	if m, ok := r.members[from]; ok {
		m.lastPost = now
	}
	r.wakeLocked(teamID)
	return msg
}

// Since returns every message in teamID's mailbox with Seq greater than
// since, oldest first.
func (r *Registry) Since(teamID string, since int) []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked(teamID)
	return r.sinceLocked(teamID, since)
}

func (r *Registry) sinceLocked(teamID string, since int) []Message {
	all := r.messages[teamID]
	idx := sort.Search(len(all), func(i int) bool { return all[i].Seq > since })
	out := make([]Message, len(all)-idx)
	copy(out, all[idx:])
	return out
}

// Wait returns messages newer than since as soon as any are available. If
// none are available yet and timeout is positive, it blocks until a new
// one is sent, ctx is done, or timeout elapses, then returns whatever is
// available at that point (possibly none).
func (r *Registry) Wait(ctx context.Context, teamID string, since int, timeout time.Duration) []Message {
	r.mu.Lock()
	r.loadLocked(teamID)
	if msgs := r.sinceLocked(teamID, since); len(msgs) > 0 || timeout <= 0 {
		r.mu.Unlock()
		return msgs
	}
	deadline := time.Now().Add(timeout)
	for {
		ch, ok := r.waiters[teamID]
		if !ok {
			ch = make(chan struct{})
			r.waiters[teamID] = ch
		}
		r.mu.Unlock()
		timer := time.NewTimer(min(time.Until(deadline), r.pollEvery()))
		select {
		case <-ch:
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		r.mu.Lock()
		r.loadLocked(teamID)
		if msgs := r.sinceLocked(teamID, since); len(msgs) > 0 || ctx.Err() != nil || !time.Now().Before(deadline) {
			r.mu.Unlock()
			return msgs
		}
	}
}

// pollEvery is how long a parked reader sleeps before checking the database
// for messages written by another process. Without a store nobody else can
// write, so it sleeps until woken.
func (r *Registry) pollEvery() time.Duration {
	if r.store == nil {
		return time.Hour
	}
	return pollInterval
}
