package model

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/terminal"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/common"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workspace"
	"github.com/stretchr/testify/require"
)

type panelFixtureWorkspace struct {
	workspace.Workspace
	stopped bool
}

func (w *panelFixtureWorkspace) WorkflowSnapshot(context.Context, string) (engineering.WorkflowSnapshot, error) {
	revision := "before"
	if w.stopped {
		revision = "STOP_REQUEST_OBSERVED"
	}
	return engineering.WorkflowSnapshot{Revision: revision, Paused: w.stopped}, nil
}

func (w *panelFixtureWorkspace) WorkflowControl(_ context.Context, _ string, control engineering.WorkflowControl) error {
	if control.Action != "stop" || control.ExpectedRevision != "before" {
		return fmt.Errorf("unexpected fixture control")
	}
	w.stopped = true
	return nil
}

// This fixture runs the production panel handlers inside a real Tea terminal.
// It does not certify other application views or a live model conversation.
type panelFixture struct {
	ui            UI
	width, height int
}

func (m *panelFixture) Init() tea.Cmd { return nil }

func (m *panelFixture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return m, nil
	}
	_, cmd := m.ui.handleWorkflowPanel(msg)
	if !m.ui.workflow.open {
		return m, tea.Quit
	}
	return m, cmd
}

func (m *panelFixture) View() tea.View {
	return tea.NewView(fmt.Sprintf("PANEL_SIZE=%dx%d\n%s\n%s", m.width, m.height, m.ui.workflow.render(m.width, max(0, m.height-2)), m.ui.workflow.snapshot.Revision))
}

func TestWorkflowPanelPTYChild(t *testing.T) {
	if os.Getenv("ATLAS_PANEL_PTY_CHILD") != "1" {
		return
	}
	m := &panelFixture{width: 80, height: 24, ui: UI{
		session:  &session.Session{ID: "fixture"},
		com:      &common.Common{Workspace: &panelFixtureWorkspace{}},
		workflow: workflowPanel{open: true, snapshot: engineering.WorkflowSnapshot{Revision: "before"}},
	}}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Println(err)
		os.Exit(91)
	}
	fmt.Println("PANEL_CLOSED")
	os.Exit(0)
}

func TestWorkflowPanelRealPTYResizeStopAndClose(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("Platform PTY unavailable")
	}
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pty, err := terminal.Start(ctx, execution.Request{Root: t.TempDir(), Argv: []string{exe, "-test.run=^TestWorkflowPanelPTYChild$"}, Env: append(os.Environ(), "ATLAS_PANEL_PTY_CHILD=1", "TERM=xterm-256color")}, execution.TerminalSize{Width: 80, Height: 24})
	require.NoError(t, err)
	defer pty.Close()
	var mu sync.Mutex
	var transcript strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := pty.Read(buf)
			mu.Lock()
			transcript.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	contains := func(text string) bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(transcript.String(), text)
	}
	require.Eventually(t, func() bool { return contains("Agent controls") }, 5*time.Second, 20*time.Millisecond)
	for _, size := range []execution.TerminalSize{{Width: 120, Height: 40}, {Width: 40, Height: 12}} {
		require.NoError(t, pty.Resize(ctx, size))
		require.Eventually(t, func() bool { return contains(fmt.Sprintf("PANEL_SIZE=%dx%d", size.Width, size.Height)) }, 3*time.Second, 20*time.Millisecond)
	}
	_, err = io.WriteString(pty, "s")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return contains("STOP_REQUEST_OBSERVED") }, 3*time.Second, 20*time.Millisecond)
	_, err = io.WriteString(pty, "\x1b")
	require.NoError(t, err)
	result, err := pty.Wait(ctx)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.NotNil(t, result.ExitCode)
	require.Zero(t, *result.ExitCode)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Panel output did not drain")
	}
	require.True(t, contains("PANEL_CLOSED"))
}
