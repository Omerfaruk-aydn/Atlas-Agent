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

func orderTools(log *eventLog) []AgentTool {
	read := NewParallelAgentTool("read", "read", func(_ context.Context, _ noInput, call ToolCall) (ToolResponse, error) {
		log.add("start " + call.ID)
		time.Sleep(50 * time.Millisecond)
		log.add("end " + call.ID)
		return NewTextResponse("ok"), nil
	})
	edit := NewAgentTool("edit", "edit", func(_ context.Context, _ noInput, call ToolCall) (ToolResponse, error) {
		log.add("start " + call.ID)
		log.add("end " + call.ID)
		return NewTextResponse("ok"), nil
	})
	return []AgentTool{read, edit}
}

// A read called before an edit in the same step finishes before the edit
// starts, and a read called after it starts only once the edit is done.
func TestToolCallsTakeEffectInCallOrder(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	model := &scriptedModel{calls: []StreamPart{
		toolCallPart("r1", "read"),
		toolCallPart("r2", "read"),
		toolCallPart("e1", "edit"),
		toolCallPart("r3", "read"),
	}}
	agent := NewAgent(model, WithTools(orderTools(log)...))
	result, err := agent.Stream(t.Context(), AgentStreamCall{Prompt: "go"})
	require.NoError(t, err)
	require.Len(t, result.Steps, 2)

	events := log.list()
	at := func(event string) int {
		i := slices.Index(events, event)
		require.GreaterOrEqual(t, i, 0, event)
		return i
	}
	require.Less(t, at("end r1"), at("start e1"))
	require.Less(t, at("end r2"), at("start e1"))
	require.Less(t, at("end e1"), at("start r3"))
	// The two reads before the edit ran at the same time.
	require.Less(t, at("start r2"), at("end r1"))

	var ids []string
	for _, content := range result.Steps[0].Content {
		if r, ok := content.(ToolResultContent); ok {
			ids = append(ids, r.ToolCallID)
		}
	}
	require.Equal(t, []string{"r1", "r2", "e1", "r3"}, ids)
}

