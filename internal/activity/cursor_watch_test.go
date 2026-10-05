package activity

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCursorWatchRestoresOnEOFAndTimeout(t *testing.T) {
	t.Parallel()
	for _, timeout := range []bool{false, true} {
		reader, writer := io.Pipe()
		done := make(chan struct{})
		restored := make(chan struct{}, 1)
		go func() {
			watchCursorOwner(reader, io.Discard, 30*time.Millisecond, func() { restored <- struct{}{} })
			close(done)
		}()
		if !timeout {
			_ = writer.Close()
		}
		select {
		case <-restored:
		case <-time.After(time.Second):
			t.Fatal("Cursor recovery did not run")
		}
		_ = writer.Close()
		<-done
	}
}

func TestCursorWatchCleanExitKeepsRestoredTheme(t *testing.T) {
	t.Parallel()
	var ready bytes.Buffer
	called := false
	watchCursorOwner(strings.NewReader("HQ"), &ready, time.Second, func() { called = true })
	require.Equal(t, "R", ready.String())
	require.False(t, called)
}
