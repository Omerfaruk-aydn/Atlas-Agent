package cmd

import (
	"bufio"
	"context"
	"io"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func TestSupportCommandUsesJournalOutcome(t *testing.T) {
	t.Parallel()
	require.NoError(t, supportCommandOperationError(nodes.Operation{State: "completed", Effect: "applied"}))
	for _, op := range []nodes.Operation{
		{State: "failed", Effect: "none", Error: "did not start"},
		{State: "failed", Effect: "unknown", Error: "backend failed"},
		{State: "running", Effect: "unknown"},
	} {
		err := supportCommandOperationError(op)
		require.Error(t, err)
		if op.Effect == "unknown" {
			require.Equal(t, support.CodeOutcomeUnknown, support.CodeOf(err))
		}
	}
}

func TestSupportDecisionCanBeCanceledBeforeInput(t *testing.T) {
	t.Parallel()
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := readSupportAnswer(ctx, bufio.NewReader(reader))
		finished <- err
	}()
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("support decision ignored cancellation while waiting for stdin")
	}
}
