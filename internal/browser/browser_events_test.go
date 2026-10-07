package browser

import (
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/stretchr/testify/require"
)

func remoteObjectValue(literal string) *runtime.RemoteObject {
	return &runtime.RemoteObject{Value: []byte(literal)}
}

func TestFormatConsoleArgsPrefersLiteralValues(t *testing.T) {
	got := formatConsoleArgs([]*runtime.RemoteObject{
		remoteObjectValue(`"hello"`),
		remoteObjectValue("42"),
	})
	require.Equal(t, "hello 42", got)
}

func TestFormatConsoleArgsFallsBackToDescriptionThenClassName(t *testing.T) {
	got := formatConsoleArgs([]*runtime.RemoteObject{
		{Description: "Error: boom"},
		{ClassName: "HTMLDivElement"},
	})
	require.Equal(t, "Error: boom HTMLDivElement", got)
}

func TestFormatConsoleArgsSkipsNilEntries(t *testing.T) {
	got := formatConsoleArgs([]*runtime.RemoteObject{nil, remoteObjectValue(`"x"`)})
	require.Equal(t, "x", got)
}

func TestFormatConsoleArgsEmpty(t *testing.T) {
	require.Empty(t, formatConsoleArgs(nil))
}

func TestAppendConsoleCapsAtMaxEntries(t *testing.T) {
	s := &chromedpSession{}
	for i := 0; i < maxConsoleEntries+10; i++ {
		s.appendConsole(ConsoleEntry{Type: "log", Text: "entry"})
	}
	logs := s.ConsoleLogs()
	require.Len(t, logs, maxConsoleEntries, "the buffer must not grow past its cap")
}

func TestConsoleLogsReturnsACopy(t *testing.T) {
	s := &chromedpSession{}
	s.appendConsole(ConsoleEntry{Type: "log", Text: "first"})

	logs := s.ConsoleLogs()
	logs[0].Text = "mutated"

	require.Equal(t, "first", s.ConsoleLogs()[0].Text, "callers must not be able to mutate session state through the returned slice")
}

func TestAppendDialogCapsAtMaxPendingDialogs(t *testing.T) {
	s := &chromedpSession{}
	for i := 0; i < maxPendingDialogs+5; i++ {
		s.appendDialog(DialogInfo{Type: "alert", Message: "hi"})
	}
	require.Len(t, s.PendingDialogs(), maxPendingDialogs)
}

func TestHandleDialogErrorsWhenNothingIsPending(t *testing.T) {
	s := &chromedpSession{}
	err := s.HandleDialog(true, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no pending dialog")
}

func TestHandleDialogDoesNotDiscardBeforeDispatchSucceeds(t *testing.T) {
	s := &chromedpSession{}
	s.appendDialog(DialogInfo{Type: "alert", Message: "first"})
	s.appendDialog(DialogInfo{Type: "confirm", Message: "second"})

	// Without a CDP target dispatch cannot succeed. The pending dialog
	// must remain observable even if the underlying call panics.
	func() {
		defer func() { _ = recover() }()
		_ = s.HandleDialog(true, "")
	}()

	require.Equal(t, []DialogInfo{{Type: "alert", Message: "first"}, {Type: "confirm", Message: "second"}}, s.PendingDialogs())
}
