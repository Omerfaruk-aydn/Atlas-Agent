package teams

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMentionsFindsNamesAllAndChannels(t *testing.T) {
	require.Equal(t, []string{"alice", "all", "#backend"}, Mentions("@Alice please look, cc @all and @#backend, @alice again"))
	require.Equal(t, []string{"bob-2"}, Mentions("(@bob-2) ok?"))
	require.Empty(t, Mentions("mail me at dev@example.com or use 100@ rate"), "an @ inside a word is not a mention")
	require.Empty(t, Mentions("no mentions here"))
}

func TestSetNameIsCleanedAndUniquePerTeam(t *testing.T) {
	r := NewRegistry()
	r.Join("root", "a")
	r.Join("root", "b")
	require.Equal(t, "code-reviewer", r.SetName("a", "Code Reviewer!"))
	require.Equal(t, "code-reviewer-2", r.SetName("b", "code reviewer"))
	require.Equal(t, "agent", r.SetName("root", "all"), "all is reserved for broadcasts")
	require.Equal(t, "code-reviewer", r.SetName("a", "code reviewer"), "renaming to its own name keeps it")

	r.Join("other-root", "c")
	require.Equal(t, "code-reviewer", r.SetName("c", "code-reviewer"), "names are unique per team, not globally")
}

func TestSendRecordsSenderNameAndAddressees(t *testing.T) {
	r := NewRegistry()
	r.Join("root", "a")
	r.SetName("a", "writer")
	msg := r.Send("root", "a", "@reviewer please check #3, cc @all")
	require.Equal(t, "writer", msg.Name)
	require.Equal(t, []string{"reviewer", "all"}, msg.To)
}

func TestReadForOnlyMineIsADoorbell(t *testing.T) {
	r := NewRegistry()
	r.Join("root", "writer")
	r.Join("root", "reviewer")
	r.SetName("writer", "writer")
	r.SetName("reviewer", "reviewer")
	r.Subscribe("reviewer", "#qa")

	r.Send("root", "writer", "just thinking aloud")     // 1: nobody addressed
	r.Send("root", "writer", "@reviewer ready for you") // 2
	r.Send("root", "root", "@writer nice")              // 3: not for the reviewer
	r.Send("root", "root", "@#qa smoke test please")    // 4: channel
	r.Send("root", "root", "@all lunch")                // 5

	got, err := r.ReadFor(t.Context(), "root", "reviewer", 0, true, 0)
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
	require.Equal(t, []int{2, 4, 5}, []int{got.Messages[0].Seq, got.Messages[1].Seq, got.Messages[2].Seq})
	require.Equal(t, 5, got.SeqAfter, "the cursor moves past bystander traffic too")

	feed, err := r.ReadFor(t.Context(), "root", "reviewer", 0, false, 0)
	require.NoError(t, err)
	require.Len(t, feed.Messages, 5, "without only_mine the reader sees the whole board")

	own, err := r.ReadFor(t.Context(), "root", "writer", 1, true, 0)
	require.NoError(t, err)
	require.Equal(t, []int{3, 5}, []int{own.Messages[0].Seq, own.Messages[1].Seq}, "a member is never paged by its own message")
}

// The usual mistake: passing the board's head as the cursor, which skips
// whatever is posted next. It is refused instead of silently losing mail.
func TestReadForRefusesACursorAheadOfTheBoard(t *testing.T) {
	r := NewRegistry()
	r.Send("root", "a", "one")
	_, err := r.ReadFor(t.Context(), "root", "b", 9, false, 5*time.Second)
	require.ErrorIs(t, err, ErrSinceAhead)
	require.ErrorContains(t, err, "#1")
}

func TestReadForParksUntilSomeoneIsAddressed(t *testing.T) {
	r := NewRegistry()
	r.Join("root", "reviewer")
	r.SetName("reviewer", "reviewer")

	var wg sync.WaitGroup
	wg.Add(1)
	var got Read
	go func() {
		defer wg.Done()
		var err error
		got, err = r.ReadFor(context.Background(), "root", "reviewer", 0, true, 5*time.Second)
		require.NoError(t, err)
	}()

	require.Eventually(t, func() bool {
		for _, m := range r.Members("root") {
			if m.Name == "reviewer" && m.State == StateWaiting {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "a parked member shows as waiting")

	r.Send("root", "writer", "chatter that is not for you")
	time.Sleep(20 * time.Millisecond)
	r.Send("root", "writer", "@reviewer now")
	wg.Wait()

	require.Len(t, got.Messages, 1, "bystander traffic did not end the wait")
	require.Equal(t, "@reviewer now", got.Messages[0].Text)
	require.Equal(t, 2, got.SeqAfter)
	for _, m := range r.Members("root") {
		if m.Name == "reviewer" {
			require.Equal(t, StateWorking, m.State, "back to working after the wait")
		}
	}
}

func TestReadForTimesOutQuietly(t *testing.T) {
	r := NewRegistry()
	start := time.Now()
	got, err := r.ReadFor(t.Context(), "root", "a", 0, true, 50*time.Millisecond)
	require.NoError(t, err)
	require.Empty(t, got.Messages)
	require.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond)
}

func TestMarkDoneWakesParkedReadersAndStopsParking(t *testing.T) {
	r := NewRegistry()
	r.Join("root", "worker")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.ReadFor(context.Background(), "root", "worker", 0, true, 10*time.Second)
	}()
	require.Eventually(t, func() bool {
		for _, m := range r.Members("root") {
			if m.ID == "worker" && m.State == StateWaiting {
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "the reader parks")
	r.MarkDone("worker")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("MarkDone did not wake the parked reader")
	}
	got, err := r.ReadFor(t.Context(), "root", "worker", 0, true, 5*time.Second)
	require.NoError(t, err)
	require.Empty(t, got.Messages, "a finished member does not park")
}

func openBoard(t *testing.T, dir string) *Registry {
	t.Helper()
	r := NewRegistry()
	require.NoError(t, r.SetStoreDir(dir))
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestStoreRestoresABoardAfterARestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "teams")
	first := openBoard(t, dir)
	first.Send("root", "a", "one")
	first.Send("root", "b", "@a two")
	require.NoError(t, first.Close())

	second := openBoard(t, dir)
	require.Equal(t, 2, second.Head("root"))
	got := second.Since("root", 0)
	require.Len(t, got, 2)
	require.Equal(t, []string{"a"}, got[1].To)
	require.Equal(t, 3, second.Send("root", "a", "three").Seq, "numbering continues where it left off")

	require.Equal(t, 0, NewRegistry().Head("root"), "no store, no restore")
	require.Equal(t, 0, second.Head("other"), "teams are separate")
}

func TestTwoProcessesShareOneBoard(t *testing.T) {
	dir := t.TempDir()
	a := openBoard(t, dir)
	b := openBoard(t, dir)

	require.Equal(t, 1, a.Send("root", "a", "from a").Seq)
	require.Equal(t, 2, b.Send("root", "b", "@a from b").Seq, "the second process continues the numbering")
	require.Equal(t, 3, a.Send("root", "a", "again").Seq)

	require.Len(t, a.Since("root", 0), 3, "a sees what b wrote")
	require.Len(t, b.Since("root", 0), 3, "b sees what a wrote")
	require.Equal(t, 3, b.Head("root"))
}

func TestParkedReaderSeesAMessageFromAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	reader := openBoard(t, dir)
	writer := openBoard(t, dir)
	reader.SetName("reader", "reader")

	go func() {
		time.Sleep(300 * time.Millisecond)
		writer.Send("root", "w", "@reader hello")
	}()
	got, err := reader.ReadFor(t.Context(), "root", "reader", 0, true, 10*time.Second)
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	require.Equal(t, "@reader hello", got.Messages[0].Text)
}

func TestConcurrentWritersNeverShareANumber(t *testing.T) {
	dir := t.TempDir()
	boards := []*Registry{openBoard(t, dir), openBoard(t, dir), openBoard(t, dir)}
	const each = 20
	var wg sync.WaitGroup
	for i, b := range boards {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				b.Send("root", fmt.Sprintf("w%d", i), "note")
			}
		}()
	}
	wg.Wait()

	all := boards[0].Since("root", 0)
	require.Len(t, all, each*len(boards))
	for i, msg := range all {
		require.Equal(t, i+1, msg.Seq, "numbers are unique and gap free")
	}
}
