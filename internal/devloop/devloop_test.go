package devloop

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	switch os.Getenv("DEVLOOP_HELPER") {
	case "server":
		// A slow-starting app: logs, waits, then serves.
		fmt.Println("booting")
		time.Sleep(400 * time.Millisecond)
		port := os.Getenv("DEVLOOP_PORT")
		fmt.Println("listening on " + port)
		_ = http.ListenAndServe("127.0.0.1:"+port, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<h1>ready</h1>"))
		}))
		os.Exit(0)
	case "crash":
		fmt.Fprintln(os.Stderr, "panic: config missing")
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func plan(t *testing.T, m *Manager) Loop {
	t.Helper()
	l, err := m.Plan("ui-fix", "s1", []Criterion{
		{ID: "saves", Text: "Saving shows a confirmation", Verify: "scenario", Assertions: []int{1, 2}},
		{ID: "email", Text: "Confirmation email arrives", Verify: "unverifiable", Reason: "no SMTP access in this environment"},
	}, 3, time.Minute)
	require.NoError(t, err)
	return l
}

func TestStartReadyAndStopOwnedApp(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	port := freePort(t)
	exe, _ := os.Executable()
	t.Setenv("DEVLOOP_HELPER", "server")
	t.Setenv("DEVLOOP_PORT", strconv.Itoa(port))
	res, err := m.Start(&l, t.TempDir(), []string{exe, "-test.run=^$"}, "", port, "http://127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	require.Equal(t, "started", res.Outcome)

	ready, err := m.Ready(t.Context(), l, Probe{Kind: "http", URL: "http://127.0.0.1:" + strconv.Itoa(port), Contains: "ready"}, 20*time.Second)
	require.NoError(t, err)
	require.True(t, ready.Ready, ready.Detail)

	logReady, err := m.Ready(t.Context(), l, Probe{Kind: "log", Pattern: `listening on \d+`}, 5*time.Second)
	require.NoError(t, err)
	require.True(t, logReady.Ready)

	again, err := m.Start(&l, t.TempDir(), []string{exe, "-test.run=^$"}, "", port, "")
	require.NoError(t, err)
	require.Equal(t, "already_running_owned", again.Outcome, "a second copy must not be started")

	outcome, err := m.Stop(&l)
	require.NoError(t, err)
	require.Equal(t, "stopped", outcome)
	require.Eventually(t, func() bool { return !Listening(port) }, 10*time.Second, 100*time.Millisecond)
}

func TestExistingUserServerIsAttachedAndNeverStopped(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	res, err := m.Start(&l, t.TempDir(), []string{"never-run"}, "", port, "")
	require.NoError(t, err)
	require.Equal(t, "attached_existing", res.Outcome)
	outcome, err := m.Stop(&l)
	require.NoError(t, err)
	require.Equal(t, "left_running_not_owned", outcome)
	require.True(t, Listening(port), "the user's dev server must keep running")
}

func TestReplanCannotDiscardOwnedProcess(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	port := freePort(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("DEVLOOP_HELPER", "server")
	t.Setenv("DEVLOOP_PORT", strconv.Itoa(port))
	_, err = m.Start(&l, t.TempDir(), []string{exe, "-test.run=^$"}, "", port, "")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Stop(&l) })
	ready, err := m.Ready(t.Context(), l, Probe{Kind: "tcp", Port: port}, 10*time.Second)
	require.NoError(t, err)
	require.True(t, ready.Ready)
	_, err = m.Plan(l.ID, l.SessionID, l.Criteria, l.MaxIterations, time.Minute)
	require.ErrorContains(t, err, "already started")
	saved, err := m.Get(l.ID)
	require.NoError(t, err)
	require.Equal(t, l.Process, saved.Process)
	outcome, err := m.Stop(&saved)
	require.NoError(t, err)
	require.Equal(t, "stopped", outcome)
	require.Eventually(t, func() bool { return !Listening(port) }, 5*time.Second, 50*time.Millisecond)
}

func TestReplanRejectsCorruptAndOtherSessionRecords(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	_, err := m.Plan(l.ID, "another-session", l.Criteria, 3, time.Minute)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(m.path(l.ID), []byte("broken"), 0o600))
	_, err = m.Plan(l.ID, l.SessionID, l.Criteria, 3, time.Minute)
	require.ErrorContains(t, err, "read existing loop")
}

func TestAppThatExitsIsReportedNotWaitedOn(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	exe, _ := os.Executable()
	t.Setenv("DEVLOOP_HELPER", "crash")
	_, err := m.Start(&l, t.TempDir(), []string{exe, "-test.run=^$"}, "", 0, "")
	require.NoError(t, err)
	res, err := m.Ready(t.Context(), l, Probe{Kind: "log", Pattern: "never"}, 30*time.Second)
	require.NoError(t, err)
	require.False(t, res.Ready)
	require.True(t, res.Exited)
	require.Equal(t, 2, res.ExitCode)
	require.Contains(t, res.LogTail, "panic: config missing")
}

func TestCriteriaAreFixedAfterTheFirstIteration(t *testing.T) {
	m := NewManager(t.TempDir())
	l := plan(t, m)
	l.Iterations = append(l.Iterations, Iteration{N: 1})
	require.NoError(t, m.Save(l))
	_, err := m.Plan("ui-fix", "s1", []Criterion{{ID: "weaker", Text: "page opens", Verify: "unverifiable", Reason: "x"}}, 3, time.Minute)
	require.Error(t, err)
	_, err = m.Plan("bad", "s1", []Criterion{{ID: "x", Text: "y", Verify: "scenario"}}, 3, time.Minute)
	require.Error(t, err, "scenario criteria must name assertions")
	l2, err := m.Plan("cap", "s1", []Criterion{{ID: "x", Text: "y", Verify: "scenario", Assertions: []int{1}}}, 99, time.Minute)
	require.NoError(t, err)
	require.Equal(t, HardMaxIterations, l2.MaxIterations)
}

func TestGradeLinksCriteriaToEvidence(t *testing.T) {
	t.Parallel()
	crit := []Criterion{
		{ID: "a", Text: "A", Verify: "scenario", Assertions: []int{1}},
		{ID: "b", Text: "B", Verify: "scenario", Assertions: []int{2, 3}},
		{ID: "c", Text: "C", Verify: "unverifiable", Reason: "external API"},
	}
	failed := FailedAssertions([]string{"Assertion failed: text #3 .toast", "source changed"})
	res, all := Grade(crit, nil, failed, true, "scenario s1", "")
	require.False(t, all)
	require.Equal(t, "passed", res[0].Result)
	require.Equal(t, "failed", res[1].Result)
	require.Contains(t, res[1].Observation, "#3")
	require.Equal(t, "unverified", res[2].Result)

	res, all = Grade(crit, errors.New("browser closed"), nil, false, "scenario s1", "")
	require.False(t, all)
	require.Contains(t, res[0].Observation, "did not complete")

	res, _ = Grade(crit, nil, nil, true, "scenario s1", "app exited with code 1")
	require.Equal(t, "failed", res[0].Result)
}
