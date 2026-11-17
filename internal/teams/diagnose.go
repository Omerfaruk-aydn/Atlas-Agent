package teams

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Board is a copy of a team's roster and messages at one moment, the input to
// Diagnose. Diagnose reads nothing else, so a stall is judged from facts that
// can be cited, not from a model's impression.
type Board struct {
	Now      time.Time
	Members  []Member
	Messages []Message
}

// Thresholds for the stall rules.
const (
	// IdleAfter is how long every active member may wait with no message
	// before the team counts as deadlocked.
	IdleAfter = 2 * time.Minute
	// UnansweredAfter is how long a message addressed to a member may go
	// without that member posting.
	UnansweredAfter = 3 * time.Minute
	// QuietAfter is how long a working member may go without touching the
	// board while others wait.
	QuietAfter = 5 * time.Minute
	// BlockedAfter is how long a "[blocked]" post may stand with nobody
	// else posting.
	BlockedAfter = 3 * time.Minute
	// PingPongRun is how many consecutive messages between the same two
	// members, with no one else, count as a loop.
	PingPongRun = 8
	// MaxAlerts bounds one diagnosis, so a stalled team gets a short list of
	// what to do first, not a wall of symptoms.
	MaxAlerts = 3
)

// Alert is one finding. It always cites the message or member it comes from.
type Alert struct {
	Rule     string
	Severity int // 1 is most urgent.
	Members  []string
	Seq      int // The message cited, 0 when the finding is about a member.
	Text     string
}

// Diagnose looks for a stalled team and returns at most MaxAlerts findings,
// most urgent first. It is a pure function of the board.
//
// An empty result means "nothing to flag", and it is also what a finished
// team gets: once the coordinator (the earliest member) has posted "[done]",
// or every member but the coordinator has finished, the monitor is dormant.
func Diagnose(b Board) []Alert {
	if len(b.Members) == 0 {
		return nil
	}
	coordinator := b.Members[0]
	for _, m := range b.Messages {
		if m.From == coordinator.ID && strings.HasPrefix(strings.TrimSpace(m.Text), "[done]") {
			return nil
		}
	}
	var active []Member
	for _, m := range b.Members {
		if m.State != StateDone {
			active = append(active, m)
		}
	}
	if len(active) == 0 || (len(active) == 1 && active[0].ID == coordinator.ID && len(b.Members) > 1) {
		return nil
	}

	var alerts []Alert
	alerts = append(alerts, deadlock(b, active)...)
	alerts = append(alerts, blocked(b)...)
	alerts = append(alerts, unanswered(b, active)...)
	alerts = append(alerts, quiet(b, active)...)
	alerts = append(alerts, pingPong(b)...)
	slices.SortStableFunc(alerts, func(a, c Alert) int { return a.Severity - c.Severity })
	if len(alerts) > MaxAlerts {
		alerts = alerts[:MaxAlerts]
	}
	return alerts
}

func head(b Board) int {
	if len(b.Messages) == 0 {
		return 0
	}
	return b.Messages[len(b.Messages)-1].Seq
}

// deadlock: every active member is parked on the board, none for a short
// while, and nothing has been posted: nobody will ever wake anybody.
func deadlock(b Board, active []Member) []Alert {
	if len(active) < 2 {
		return nil
	}
	names := make([]string, 0, len(active))
	for _, m := range active {
		if m.State != StateWaiting || b.Now.Sub(m.Since) < IdleAfter {
			return nil
		}
		names = append(names, "@"+m.Name)
	}
	if n := len(b.Messages); n > 0 && b.Now.Sub(b.Messages[n-1].Time) < IdleAfter {
		return nil
	}
	return []Alert{{
		Rule: "deadlock", Severity: 1, Seq: head(b),
		Members: memberNames(active),
		Text:    fmt.Sprintf("Everyone is waiting (%s) and nothing has been posted since #%d. Someone must post a result, a question or [done].", strings.Join(names, ", "), head(b)),
	}}
}

// blocked: a "[blocked]" post nobody answered.
func blocked(b Board) []Alert {
	var out []Alert
	for i, m := range b.Messages {
		if !strings.HasPrefix(strings.TrimSpace(m.Text), "[blocked]") || b.Now.Sub(m.Time) < BlockedAfter {
			continue
		}
		answered := slices.ContainsFunc(b.Messages[i+1:], func(later Message) bool { return later.From != m.From })
		if !answered {
			out = append(out, Alert{
				Rule: "blocked", Severity: 2, Seq: m.Seq, Members: []string{m.Name},
				Text: fmt.Sprintf("@%s reported [blocked] in #%d and nobody has replied.", m.Name, m.Seq),
			})
		}
	}
	return out
}

// unanswered: a message addressed to a member by name, older than the
// threshold, and the member has not posted since. @all and channel posts are
// broadcasts and are not chased.
func unanswered(b Board, active []Member) []Alert {
	var out []Alert
	for _, m := range active {
		for _, msg := range b.Messages {
			if msg.From == m.ID || !slices.Contains(msg.To, m.Name) || b.Now.Sub(msg.Time) < UnansweredAfter {
				continue
			}
			if m.LastPost.After(msg.Time) {
				continue
			}
			out = append(out, Alert{
				Rule: "unanswered", Severity: 3, Seq: msg.Seq, Members: []string{m.Name},
				Text: fmt.Sprintf("@%s was addressed in #%d %s ago and has not posted since.", m.Name, msg.Seq, b.Now.Sub(msg.Time).Round(time.Second)),
			})
			break // The oldest unanswered message is the one to cite.
		}
	}
	return out
}

// quiet: a working member has not touched the board for a long time while
// the rest of the team is waiting on it.
func quiet(b Board, active []Member) []Alert {
	waiting := 0
	for _, m := range active {
		if m.State == StateWaiting {
			waiting++
		}
	}
	if waiting == 0 {
		return nil
	}
	var out []Alert
	for _, m := range active {
		if m.State != StateWorking {
			continue
		}
		last := latest(m.Joined, m.LastPost, m.LastRead, m.Since)
		if b.Now.Sub(last) < QuietAfter {
			continue
		}
		out = append(out, Alert{
			Rule: "quiet", Severity: 4, Members: []string{m.Name},
			Text: fmt.Sprintf("@%s has not touched the board for %s while %d member(s) wait.", m.Name, b.Now.Sub(last).Round(time.Second), waiting),
		})
	}
	return out
}

func latest(times ...time.Time) time.Time {
	var out time.Time
	for _, t := range times {
		if t.After(out) {
			out = t
		}
	}
	return out
}

// pingPong: the last PingPongRun messages alternate between the same two
// members with no one else and no "[result]" marker: a conversation going in
// circles.
func pingPong(b Board) []Alert {
	n := len(b.Messages)
	if n < PingPongRun {
		return nil
	}
	tail := b.Messages[n-PingPongRun:]
	a, c := tail[0].From, ""
	for i, m := range tail {
		if strings.HasPrefix(strings.TrimSpace(m.Text), "[result]") {
			return nil
		}
		switch {
		case i == 0:
		case i == 1:
			if m.From == a {
				return nil
			}
			c = m.From
		case i%2 == 0 && m.From != a, i%2 == 1 && m.From != c:
			return nil
		}
	}
	return []Alert{{
		Rule: "ping-pong", Severity: 5, Seq: tail[len(tail)-1].Seq, Members: []string{tail[0].Name, tail[1].Name},
		Text: fmt.Sprintf("@%s and @%s have traded %d messages (#%d to #%d) with no result. Decide or escalate to the coordinator.", tail[0].Name, tail[1].Name, PingPongRun, tail[0].Seq, tail[len(tail)-1].Seq),
	}}
}

func memberNames(ms []Member) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}
