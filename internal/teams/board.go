package teams

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
)

// State is what a team member is doing right now, as far as the board can
// tell: working between board calls, waiting for a message, or finished.
type State string

const (
	StateWorking State = "working"
	StateWaiting State = "waiting"
	StateDone    State = "done"
)

// member is one agent on a team's board.
type member struct {
	team     string
	name     string
	channels map[string]bool
	state    State
	since    time.Time // When state last changed.
	lastPost time.Time
	lastRead time.Time
	joined   time.Time
	order    int // Join order; the earliest member is the lead.
}

// Member is a read-only copy of a member for roster and diagnosis.
type Member struct {
	ID       string
	Name     string
	Channels []string
	State    State
	Since    time.Time
	LastPost time.Time
	LastRead time.Time
	Joined   time.Time
	Order    int
}

// ErrSinceAhead means a reader passed a cursor beyond the newest message,
// which would silently skip every message posted from then on. The usual
// cause is using the board's head as `since` instead of the seq_after of the
// last read.
var ErrSinceAhead = errors.New("since is ahead of the newest message")

var mentionRe = regexp.MustCompile(`(?:^|[^\w@])@(#?[A-Za-z][A-Za-z0-9_-]{0,62})`)

// Mentions lists who a message is addressed to: @name for a member, @all for
// everyone, @#channel for a channel's subscribers. The result is lowercase and
// without repeats. An @ inside a word (an email address) is not a mention.
func Mentions(text string) []string {
	var out []string
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		target := strings.ToLower(m[1])
		if !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out
}

var nameClean = regexp.MustCompile(`[^a-z0-9-]+`)

// SetName gives a session a short, unique handle on its team's board, so
// others can address it with @name. It returns the handle actually assigned:
// the requested name cleaned up, with -2, -3 ... added when it is taken.
func (r *Registry) SetName(sessionID, name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.memberLocked(sessionID)
	base := strings.Trim(nameClean.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if base == "" || base == "all" {
		base = "agent"
	}
	if len(base) > 32 {
		base = strings.Trim(base[:32], "-")
	}
	candidate := base
	for n := 2; r.nameTakenLocked(m.team, candidate, sessionID); n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	m.name = candidate
	return candidate
}

func (r *Registry) nameTakenLocked(team, name, except string) bool {
	for id, m := range r.members {
		if id != except && m.team == team && m.name == name {
			return true
		}
	}
	return false
}

// NameIfUnset gives a session a handle only if it has none yet, so a lead that
// was named once keeps its handle across every later sub-agent it starts.
func (r *Registry) NameIfUnset(sessionID, name string) {
	r.mu.Lock()
	named := r.memberLocked(sessionID).name != ""
	r.mu.Unlock()
	if !named {
		r.SetName(sessionID, name)
	}
}

// Name returns a session's handle, or a short form of its id when it has none.
func (r *Registry) Name(sessionID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nameLocked(sessionID)
}

func (r *Registry) nameLocked(sessionID string) string {
	if m, ok := r.members[sessionID]; ok && m.name != "" {
		return m.name
	}
	if len(sessionID) > 8 {
		return sessionID[:8]
	}
	return sessionID
}

// Subscribe adds a session to a channel, so @#channel reaches it.
func (r *Registry) Subscribe(sessionID, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.memberLocked(sessionID).channels[strings.ToLower(strings.TrimPrefix(channel, "#"))] = true
}

// memberLocked returns the session's member record, creating it on first use.
func (r *Registry) memberLocked(sessionID string) *member {
	if m, ok := r.members[sessionID]; ok {
		return m
	}
	team, ok := r.memberOf[sessionID]
	if !ok {
		team = sessionID
		r.memberOf[sessionID] = team
	}
	now := r.clock()
	r.nextOrder++
	m := &member{team: team, channels: map[string]bool{}, state: StateWorking, since: now, joined: now, order: r.nextOrder}
	r.members[sessionID] = m
	return m
}

func (r *Registry) setStateLocked(m *member, s State) {
	if m.state != s {
		m.state, m.since = s, r.clock()
	}
}

// MarkDone records that a session has finished its work. A finished member is
// never reported as stalled, and anyone parked on the board is woken so a
// reader waiting on a finished member does not wait out its timeout.
func (r *Registry) MarkDone(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[sessionID]
	if !ok {
		return
	}
	r.setStateLocked(m, StateDone)
	r.wakeLocked(m.team)
}

// Head returns the newest message's sequence number, 0 for an empty board.
func (r *Registry) Head(teamID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked(teamID)
	return r.counter[teamID]
}

// addressedToLocked reports whether msg is meant for the session: it names the
// session, names everyone, or names a channel the session subscribes to. A
// member is never addressed by its own message.
func (r *Registry) addressedToLocked(msg Message, sessionID string) bool {
	if msg.From == sessionID {
		return false
	}
	m := r.memberLocked(sessionID)
	for _, to := range msg.To {
		switch {
		case to == "all", to == m.name && m.name != "":
			return true
		case strings.HasPrefix(to, "#") && m.channels[strings.TrimPrefix(to, "#")]:
			return true
		}
	}
	return false
}

// Read is the result of a board read: the messages that concern the reader,
// and where to continue from.
type Read struct {
	// Messages are the new messages addressed to the reader (all of them when
	// the reader asked for the whole board).
	Messages []Message
	// SeqAfter is the cursor to pass next time. It is the newest message the
	// reader has now seen past, addressed to them or not, so a reader that
	// only wants its own mail is not handed the same bystander traffic again.
	SeqAfter int
}

// ReadFor returns what is new on the board for sessionID since the cursor.
// With onlyMine it returns only messages addressed to the session (a
// doorbell, not a feed). If nothing qualifies and timeout is positive it
// parks until something does, the context ends or the timeout passes; while
// parked the member shows as waiting.
//
// A cursor ahead of the newest message is refused with ErrSinceAhead rather
// than parking: it would skip every message posted between the real head
// and that cursor.
func (r *Registry) ReadFor(ctx context.Context, teamID, sessionID string, since int, onlyMine bool, timeout time.Duration) (Read, error) {
	r.mu.Lock()
	r.loadLocked(teamID)
	since = max(since, 0)
	if head := r.counter[teamID]; since > head {
		r.mu.Unlock()
		return Read{SeqAfter: head}, fmt.Errorf("%w: since=%d but the newest message is #%d; pass the seq_after of your last read", ErrSinceAhead, since, head)
	}
	m := r.memberLocked(sessionID)
	m.lastRead = r.clock()
	collect := func() Read {
		r.loadLocked(teamID)
		out := Read{SeqAfter: since}
		for _, msg := range r.sinceLocked(teamID, since) {
			out.SeqAfter = msg.Seq
			if !onlyMine || r.addressedToLocked(msg, sessionID) {
				out.Messages = append(out.Messages, msg)
			}
		}
		return out
	}
	got := collect()
	if len(got.Messages) > 0 || timeout <= 0 || m.state == StateDone {
		r.mu.Unlock()
		return got, nil
	}
	r.setStateLocked(m, StateWaiting)
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
		got = collect()
		if len(got.Messages) > 0 || ctx.Err() != nil || m.state == StateDone || !time.Now().Before(deadline) {
			break
		}
	}
	if m.state == StateWaiting {
		r.setStateLocked(m, StateWorking)
	}
	m.lastRead = r.clock()
	r.mu.Unlock()
	return got, nil
}

// wakeLocked wakes every reader parked on the team's board.
func (r *Registry) wakeLocked(teamID string) {
	if ch, ok := r.waiters[teamID]; ok {
		close(ch)
		delete(r.waiters, teamID)
	}
}

// Members returns the team's roster, ordered by when each member joined.
func (r *Registry) Members(teamID string) []Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.membersLocked(teamID)
}

func (r *Registry) membersLocked(teamID string) []Member {
	var out []Member
	for id, m := range r.members {
		if m.team != teamID {
			continue
		}
		channels := make([]string, 0, len(m.channels))
		for c := range m.channels {
			channels = append(channels, c)
		}
		slices.Sort(channels)
		out = append(out, Member{ID: id, Name: r.nameLocked(id), Channels: channels, State: m.state, Since: m.since, LastPost: m.lastPost, LastRead: m.lastRead, Joined: m.joined, Order: m.order})
	}
	slices.SortFunc(out, func(a, b Member) int {
		return a.Order - b.Order
	})
	return out
}

// Snapshot copies a team's roster and messages for diagnosis.
func (r *Registry) Snapshot(teamID string) Board {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadLocked(teamID)
	return Board{Now: r.clock(), Members: r.membersLocked(teamID), Messages: r.sinceLocked(teamID, 0)}
}

// SetStoreDir makes the board durable: every message is kept in a SQLite
// database in dir, and a team's earlier messages come back the first time the
// team is used after a restart. Several processes may share one directory;
// each sees the others' messages, and message numbers never collide. A store
// that cannot be opened never blocks messaging: the board keeps working in
// memory and the error is returned so the caller can log it.
func (r *Registry) SetStoreDir(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		_ = r.store.close()
	}
	r.store = newStore(dir)
	r.loaded = make(map[string]bool)
	return nil
}

// Close releases the board's database, if it has one.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store == nil {
		return nil
	}
	err := r.store.close()
	r.store = nil
	return err
}

// loadLocked brings the in-memory board up to date with the database: the
// first call for a team restores its history, later calls pick up messages
// other processes wrote. Without a store there is nothing to load.
func (r *Registry) loadLocked(teamID string) {
	if r.store == nil {
		return
	}
	fresh, err := r.store.after(teamID, r.counter[teamID])
	if err != nil {
		slog.Warn("Could not read the team board database", "error", err)
		return
	}
	for _, msg := range fresh {
		r.messages[teamID] = append(r.messages[teamID], msg)
		r.counter[teamID] = msg.Seq
	}
	if len(fresh) > 0 && r.loaded[teamID] {
		r.wakeLocked(teamID)
	}
	r.loaded[teamID] = true
}
