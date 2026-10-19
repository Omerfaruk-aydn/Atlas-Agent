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

func (m *scriptedModel) Generate(context.Context, Call) (*Response, error) {
	return nil, errors.New("not used")
}

func (m *scriptedModel) GenerateObject(context.Context, ObjectCall) (*ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *scriptedModel) StreamObject(context.Context, ObjectCall) (ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}
func (m *scriptedModel) Provider() string { return "test" }
func (m *scriptedModel) Model() string    { return "test" }

type noInput struct{}

func toolCallPart(id, name string) StreamPart {
	return StreamPart{Type: StreamPartTypeToolCall, ID: id, ToolCallName: name, ToolCallInput: `{}`}
}

// eventLog records what the tools did, in order.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

