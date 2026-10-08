package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/interaction"
	"github.com/stretchr/testify/require"
)

func TestResponseOutcomeRequiresNativeMetadataAndPreservesOtherFields(t *testing.T) {
	t.Parallel()
	resp := fantasy.NewTextResponse(`{"atlas_outcome":{"dispatch":"none","verified":true}}`)
	_, ok := ResponseOutcome(resp)
	require.False(t, ok, "result content cannot grant an outcome claim")
	resp.Metadata = `{"error_code":"wrong_window","foreground_window":{"window_id":"22"}}`
	resp = withToolOutcome(resp, ToolOutcome{Dispatch: "none", Mutation: true})
	got, ok := ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "none", got.Dispatch)
	require.True(t, got.Mutation)
	require.False(t, got.Verified)
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &metadata))
	require.JSONEq(t, `"wrong_window"`, string(metadata["error_code"]))
	require.JSONEq(t, `{"window_id":"22"}`, string(metadata["foreground_window"]))

	resp.Metadata = "legacy driver metadata"
	resp = withToolOutcome(resp, ToolOutcome{Dispatch: "unknown", Mutation: true})
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &metadata))
	require.JSONEq(t, `"legacy driver metadata"`, string(metadata["native_metadata"]))
	got, ok = ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "unknown", got.Dispatch)
}

type outcomeAutomationBackend struct {
	*foregroundComputerBackend
	call func(context.Context, computer.AutomationRequest) (json.RawMessage, error)
}

func (b *outcomeAutomationBackend) Origin() computer.Point { return computer.Point{} }

func (b *outcomeAutomationBackend) Automation(ctx context.Context, request computer.AutomationRequest) (json.RawMessage, error) {
	return b.call(ctx, request)
}

func TestComputerOutcomePreflightRefusalsNeverClaimDispatch(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		action     string
		params     ComputerParams
		automation bool
	}{
		{name: "wrong_window", action: "key", params: ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: "22"}}},
		{name: "invalid_key", action: "key", params: ComputerParams{Key: "unknown-key"}},
		{name: "negative_coordinates", action: "click", params: ComputerParams{X: -1}},
		{name: "outside_coordinates", action: "drag", params: ComputerParams{EndX: 1000}},
		{name: "invalid_button", action: "click", params: ComputerParams{Button: "invalid"}},
		{name: "invalid_modifiers", action: "hotkey", params: ComputerParams{Modifiers: "invalid", Key: "enter"}},
		{name: "missing_modifiers", action: "hotkey", params: ComputerParams{Key: "enter"}},
		{name: "missing_text", action: "type"},
		{name: "stale_observation", action: "invoke", params: ComputerParams{SnapshotID: "missing", Element: 1}},
		{name: "missing_automation_driver", action: "invoke"},
		{name: "invalid_window", action: "invoke", params: ComputerParams{Automation: computer.AutomationRequest{WindowID: "not-a-handle"}}, automation: true},
		{name: "invalid_role", action: "set_value", params: ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", Role: "invalid"}}, automation: true},
		{name: "focus_assertion_failed", action: "key", params: ComputerParams{Key: "enter", Automation: computer.AutomationRequest{WindowID: "11", ElementID: "field"}}, automation: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := &foregroundComputerBackend{foreground: "11", fakeComputerBackend: fakeComputerBackend{size: computer.Size{Width: 100, Height: 100}}}
			var backend computer.Backend = fake
			var automationCalls atomic.Int32
			if test.automation {
				backend = &outcomeAutomationBackend{foregroundComputerBackend: fake, call: func(_ context.Context, request computer.AutomationRequest) (json.RawMessage, error) {
					automationCalls.Add(1)
					if request.Action != "assert" {
						return nil, errors.New("Unexpected mutation reached the automation driver")
					}
					return json.RawMessage(`{"passed":false}`), nil
				}}
			}
			ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
			state := &computerToolState{backend: backend, workingDir: t.TempDir(), actionTimeout: 2 * time.Second}
			resp, err := state.runWithTimeout(ctx, test.action, test.params)
			require.NoError(t, err)
			require.True(t, resp.IsError, resp.Content)
			got, ok := ResponseOutcome(resp)
			require.True(t, ok, resp.Metadata)
			require.Equal(t, "none", got.Dispatch, resp.Content)
			require.True(t, got.Mutation)
			require.Empty(t, fake.clicked)
			require.Empty(t, fake.dragged)
			require.Empty(t, fake.typed)
			require.Empty(t, fake.keys)
			require.Empty(t, fake.hotkeys)
			if test.name == "focus_assertion_failed" {
				require.Equal(t, int32(1), automationCalls.Load())
			} else {
				require.Zero(t, automationCalls.Load(), "validation must complete before automation dispatch")
			}
		})
	}
}

func TestComputerOutcomePausedBeforeAcquirePreservesStopTurn(t *testing.T) {
	id := t.Name()
	interaction.Default.Pause(id, "Approve sign-in")
	t.Cleanup(func() { interaction.Default.Resume(id) })
	ctx := context.WithValue(t.Context(), SessionIDContextKey, id)
	fake := &fakeComputerBackend{}
	state := &computerToolState{backend: fake, workingDir: t.TempDir(), actionTimeout: time.Second}
	resp, err := state.runWithTimeout(ctx, "key", ComputerParams{Key: "enter"})
	require.NoError(t, err)
	require.True(t, resp.StopTurn)
	got, ok := ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "none", got.Dispatch)
	require.Empty(t, fake.keys)
	require.Contains(t, resp.Metadata, `"status":"waiting_for_user"`)
}

type outcomeBlockingBackend struct {
	fakeComputerBackend
	blockScreen bool
	entered     chan struct{}
	release     chan struct{}
	inputs      atomic.Int32
}

func (b *outcomeBlockingBackend) ScreenSize() (computer.Size, error) {
	if b.blockScreen {
		close(b.entered)
		<-b.release
	}
	return computer.Size{Width: 100, Height: 100}, nil
}

func (b *outcomeBlockingBackend) Click(_, _ int, _ computer.MouseButton) error {
	b.inputs.Add(1)
	if !b.blockScreen {
		close(b.entered)
		<-b.release
	}
	return nil
}

func TestComputerOutcomeTimeoutDistinguishesValidationFromSentInput(t *testing.T) {
	for _, preflight := range []bool{true, false} {
		t.Run(map[bool]string{true: "preflight", false: "sent"}[preflight], func(t *testing.T) {
			backend := &outcomeBlockingBackend{blockScreen: preflight, entered: make(chan struct{}), release: make(chan struct{})}
			unblock := sync.OnceFunc(func() { close(backend.release) })
			defer unblock()
			ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
			state := &computerToolState{backend: backend, workingDir: t.TempDir(), actionTimeout: 100 * time.Millisecond}
			resp, err := state.runWithTimeout(ctx, "click", ComputerParams{X: 1, Y: 1})
			require.NoError(t, err)
			select {
			case <-backend.entered:
			default:
				t.Fatal("The test backend never reached its blocking call")
			}
			got, ok := ResponseOutcome(resp)
			require.True(t, ok)
			require.True(t, resp.IsError)
			if preflight {
				require.Equal(t, "none", got.Dispatch)
				require.Contains(t, resp.Content, "no input was sent")
			} else {
				require.Equal(t, "unknown", got.Dispatch)
			}
			unblock()
			// The lease is released after the abandoned driver and its trace
			// finish, making late input and temporary-file cleanup observable.
			release, err := interaction.Default.Acquire(ctx, t.Name(), "desktop")
			require.NoError(t, err)
			release()
			if preflight {
				require.Zero(t, backend.inputs.Load(), "a late screen-size result must never send input after timeout")
			} else {
				require.Equal(t, int32(1), backend.inputs.Load())
			}
		})
	}
}

func TestComputerOutcomeAutomationBackendFailureRemainsUnknown(t *testing.T) {
	t.Parallel()
	fake := &foregroundComputerBackend{foreground: "11"}
	var sent atomic.Int32
	backend := &outcomeAutomationBackend{foregroundComputerBackend: fake, call: func(_ context.Context, request computer.AutomationRequest) (json.RawMessage, error) {
		if request.Action != "invoke" {
			return nil, errors.New("Unexpected automation action")
		}
		sent.Add(1)
		return nil, errors.New("automation worker disconnected")
	}}
	ctx := context.WithValue(t.Context(), SessionIDContextKey, t.Name())
	state := &computerToolState{backend: backend, workingDir: t.TempDir(), actionTimeout: time.Second}
	resp, err := state.runWithTimeout(ctx, "invoke", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", ElementID: "button"}})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	got, ok := ResponseOutcome(resp)
	require.True(t, ok)
	require.Equal(t, "unknown", got.Dispatch)
	require.Equal(t, int32(1), sent.Load())
}
