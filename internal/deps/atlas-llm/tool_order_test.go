package fantasy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// scriptedModel streams the given tool calls on its first step and stops on
// the next one.
type scriptedModel struct {
	calls []StreamPart
	steps atomic.Int32
}

func (m *scriptedModel) Stream(context.Context, Call) (StreamResponse, error) {
	first := m.steps.Add(1) == 1
	return func(yield func(StreamPart) bool) {
		if first {
			for _, part := range m.calls {
				if !yield(part) {
					return
				}
			}
			yield(StreamPart{Type: StreamPartTypeFinish, FinishReason: FinishReasonToolCalls})
			return
		}
		yield(StreamPart{Type: StreamPartTypeFinish, FinishReason: FinishReasonStop})
	}, nil
}

