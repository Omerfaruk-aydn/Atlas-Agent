package teams

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

var epoch = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// board builds a team whose clock is `at` minutes after the epoch.
func board(at int, members []Member, messages ...Message) Board {
	return Board{Now: epoch.Add(time.Duration(at) * time.Minute), Members: members, Messages: messages}
}

func mem(id string, state State, sinceMin int) Member {
	t := epoch.Add(time.Duration(sinceMin) * time.Minute)
	return Member{ID: id, Name: id, State: state, Since: t, Joined: epoch}
}

func at(min int) time.Time { return epoch.Add(time.Duration(min) * time.Minute) }

func rules(alerts []Alert) []string {
	out := make([]string, len(alerts))
	for i, a := range alerts {
		out[i] = a.Rule
	}
	return out
}

func TestDiagnoseHealthyTeamHasNothingToFlag(t *testing.T) {
	b := board(1,
		[]Member{mem("lead", StateWorking, 0), mem("dev", StateWorking, 0)},
		Message{Seq: 1, From: "lead", Name: "lead", Text: "@dev start", To: []string{"dev"}, Time: at(0)})
	require.Empty(t, Diagnose(b))
	require.Empty(t, Diagnose(Board{}))
}

func TestDiagnoseDeadlockWhenEveryoneWaitsAndNothingIsPosted(t *testing.T) {
	b := board(10,
		[]Member{mem("lead", StateWaiting, 5), mem("dev", StateWaiting, 6)},
		Message{Seq: 1, From: "lead", Name: "lead", Text: "@dev go", To: []string{"dev"}, Time: at(3)},
		Message{Seq: 2, From: "dev", Name: "dev", Text: "@lead done?", To: []string{"lead"}, Time: at(4)})
	got := Diagnose(b)
	require.Equal(t, "deadlock", got[0].Rule)
	require.Equal(t, 2, got[0].Seq, "cites the last message")
	require.Contains(t, got[0].Text, "#2")

	// A recent message, or one member still working, is not a deadlock.
	recent := board(5, b.Members, b.Messages...)
	require.NotContains(t, rules(Diagnose(recent)), "deadlock")
	b.Members[1].State = StateWorking
	require.NotContains(t, rules(Diagnose(b)), "deadlock")
}

func TestDiagnoseUnansweredMentionCitesTheOldestMessage(t *testing.T) {
	dev := mem("dev", StateWorking, 0)
	dev.LastPost = at(1)
	b := board(10,
		[]Member{mem("lead", StateWorking, 0), dev},
		Message{Seq: 1, From: "lead", Name: "lead", Text: "@dev first", To: []string{"dev"}, Time: at(2)},
		Message{Seq: 2, From: "lead", Name: "lead", Text: "@dev second", To: []string{"dev"}, Time: at(4)})
	got := Diagnose(b)
	require.Equal(t, []string{"unanswered"}, rules(got))
	require.Equal(t, 1, got[0].Seq)
	require.Equal(t, []string{"dev"}, got[0].Members)

	dev.LastPost = at(5) // It answered after both.
	b.Members[1] = dev
	require.Empty(t, Diagnose(b))

	// @all is a broadcast, never chased.
	b2 := board(20, []Member{mem("lead", StateWorking, 0), mem("dev", StateWorking, 0)},
		Message{Seq: 1, From: "lead", Name: "lead", Text: "@all heads up", To: []string{"all"}, Time: at(1)})
	require.Empty(t, Diagnose(b2))
}

func TestDiagnoseBlockedNeedsSomeoneElseToReply(t *testing.T) {
	members := []Member{mem("lead", StateWorking, 0), mem("dev", StateWorking, 0)}
	stuck := Message{Seq: 1, From: "dev", Name: "dev", Text: "[blocked] need the API key", Time: at(1)}
	got := Diagnose(board(10, members, stuck))
	require.Equal(t, "blocked", got[0].Rule)
	require.Equal(t, 1, got[0].Seq)

	reply := Message{Seq: 2, From: "lead", Name: "lead", Text: "use the staging one", Time: at(2)}
	require.NotContains(t, rules(Diagnose(board(10, members, stuck, reply))), "blocked")
	require.Empty(t, Diagnose(board(2, members, stuck)), "too early to call it stuck")
}

func TestDiagnoseQuietWorkerWhileOthersWait(t *testing.T) {
	b := board(20, []Member{mem("lead", StateWaiting, 18), mem("dev", StateWorking, 0)})
	got := Diagnose(b)
	require.Equal(t, []string{"quiet"}, rules(got))
	require.Equal(t, []string{"dev"}, got[0].Members)

	// Nobody waiting on it: a long job is not a stall.
	b.Members[0].State = StateWorking
	require.Empty(t, Diagnose(b))
}

func TestDiagnosePingPong(t *testing.T) {
	members := []Member{mem("lead", StateWorking, 0), mem("a", StateWorking, 0), mem("b", StateWorking, 0)}
	var msgs []Message
	for i := 1; i <= PingPongRun; i++ {
		from := "a"
		if i%2 == 0 {
			from = "b"
		}
		msgs = append(msgs, Message{Seq: i, From: from, Name: from, Text: "what do you think?", Time: at(i)})
	}
	got := Diagnose(board(PingPongRun+1, members, msgs...))
	require.Equal(t, []string{"ping-pong"}, rules(got))
	require.Equal(t, PingPongRun, got[0].Seq)

	msgs[3].Text = "[result] we agree on X"
	require.Empty(t, Diagnose(board(PingPongRun+1, members, msgs...)), "a result ends the loop")

	msgs[3].Text = "still going"
	msgs[5].From, msgs[5].Name = "lead", "lead"
	require.Empty(t, Diagnose(board(PingPongRun+1, members, msgs...)), "a third voice breaks the pattern")
}

func TestDiagnoseIsDormantOnceTheCoordinatorPostsDone(t *testing.T) {
	members := []Member{mem("lead", StateWaiting, 0), mem("dev", StateWaiting, 0)}
	stuck := Message{Seq: 1, From: "dev", Name: "dev", Text: "[blocked] x", Time: at(1)}
	require.NotEmpty(t, Diagnose(board(30, members, stuck)))
	done := Message{Seq: 2, From: "lead", Name: "lead", Text: "[done] shipped", Time: at(2)}
	require.Empty(t, Diagnose(board(30, members, stuck, done)))

	// A "[done]" from someone other than the coordinator does not silence it.
	fake := Message{Seq: 2, From: "dev", Name: "dev", Text: "[done] me", Time: at(2)}
	require.NotEmpty(t, Diagnose(board(30, members, stuck, fake)))

	// Everyone but the coordinator finished: nothing left to stall.
	members[1].State = StateDone
	require.Empty(t, Diagnose(board(30, members, stuck)))
}

func TestDiagnoseReportsAtMostThreeMostUrgentFirst(t *testing.T) {
	members := []Member{mem("lead", StateWaiting, 0)}
	var msgs []Message
	for i, name := range []string{"a", "b", "c", "d"} {
		m := mem(name, StateWorking, 0)
		members = append(members, m)
		msgs = append(msgs, Message{Seq: i + 1, From: "lead", Name: "lead", Text: "@" + name + " ping", To: []string{name}, Time: at(1)})
	}
	msgs = append(msgs, Message{Seq: 5, From: "a", Name: "a", Text: "[blocked] help", Time: at(1)})
	got := Diagnose(board(30, members, msgs...))
	require.Len(t, got, MaxAlerts)
	require.Equal(t, "blocked", got[0].Rule, "blocked outranks unanswered")
	for i := 1; i < len(got); i++ {
		require.GreaterOrEqual(t, got[i].Severity, got[i-1].Severity)
	}
}

// Diagnose works on a live registry too, driven by an injected clock.
func TestSnapshotFeedsDiagnose(t *testing.T) {
	r := NewRegistry()
	now := epoch
	r.clock = func() time.Time { return now }
	r.Join("lead", "dev")
	r.SetName("lead", "lead")
	r.SetName("dev", "dev")
	r.Send("lead", "lead", "@dev start the importer")

	now = epoch.Add(10 * time.Minute)
	got := Diagnose(r.Snapshot("lead"))
	require.Equal(t, []string{"unanswered"}, rules(got))
	require.Equal(t, "dev", got[0].Members[0])

	r.Send("lead", "dev", "[result] importer started")
	require.Empty(t, Diagnose(r.Snapshot("lead")))
}
