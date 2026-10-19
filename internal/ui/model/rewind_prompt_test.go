package model

import (
	"context"
	"reflect"
	"testing"

	tea "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-ui/v2"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/message"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/rewind"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

type rewindWorkspace struct {
	*countingWorkspace
	sent []string
}

func (w *rewindWorkspace) ListMessages(context.Context, string) ([]message.Message, error) {
	return nil, nil
}

func (w *rewindWorkspace) ListUserMessages(context.Context, string) ([]message.Message, error) {
	return nil, nil
}

func (w *rewindWorkspace) AgentReadyErr() error { return nil }

func (w *rewindWorkspace) AgentRun(_ context.Context, sessionID, prompt string, _ ...message.Attachment) error {
	w.sent = append(w.sent, sessionID+": "+prompt)
	return nil
}

func (w *rewindWorkspace) FileTrackerRecordRead(context.Context, string, string) {}

// runAll runs cmd and every command it batches or sequences, without
// feeding the resulting messages back.
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		for i := range v.Len() {
			runAll(v.Index(i).Interface().(tea.Cmd))
		}
	}
}

// A rewind only loads the fork: nothing is sent to the agent and the
// editor is left alone.
func TestRewindDoesNotRunTheAgent(t *testing.T) {
	pinTTLs(t)

	ws := &rewindWorkspace{countingWorkspace: &countingWorkspace{ready: true}}
	m := newBusyUI(ws.countingWorkspace)
	m.com.Workspace = ws
	fork := session.Session{ID: "fork"}

	m.dispatchDialogAction(dialog.ActionRewindApplied{Result: rewind.Result{Session: fork}})
	_, cmd := m.Update(loadSessionMsg{session: &fork})
	runAll(cmd)
	require.Empty(t, ws.sent)
	require.Empty(t, m.textarea.Value())
}

// Messages copied by a rewind are published as created events that can
// arrive after the fork has loaded; they must not be appended again.
func TestLateEventsForLoadedMessagesAreIgnored(t *testing.T) {
	pinTTLs(t)

	ws := &rewindWorkspace{countingWorkspace: &countingWorkspace{ready: true}}
	m := newBusyUI(ws.countingWorkspace)
	m.com.Workspace = ws
	m.session = &session.Session{ID: "fork"}
	call := message.Message{ID: "a1", SessionID: "fork", Role: message.Assistant, Parts: []message.ContentPart{
		message.ToolCall{ID: "call-1", Name: "write", Input: "{}", Finished: true},
		message.Finish{Reason: message.FinishReasonToolUse},
	}}
	result := message.Message{ID: "t1", SessionID: "fork", Role: message.Tool, Parts: []message.ContentPart{
		message.ToolResult{ToolCallID: "call-1", Name: "write", Content: "ok"},
	}}
	m.setSessionMessages([]message.Message{call, result})
	before := m.chat.Len()

	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: call})
	m.Update(pubsub.Event[message.Message]{Type: pubsub.CreatedEvent, Payload: result})
	require.Equal(t, before, m.chat.Len())
}
