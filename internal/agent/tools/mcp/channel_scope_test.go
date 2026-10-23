package mcp

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func requireScopedChannel(t *testing.T, events <-chan pubsub.Event[Event], name string) Event {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event := <-events:
			if event.Payload.Type == EventChannelMessage && event.Payload.Name == name {
				return event.Payload
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for scoped channel event")
		}
	}
}

func TestWorkspaceChannelRoutingDropsOtherAndUnknownWorkspaces(t *testing.T) {
	first := SubscribeWorkspaceEvents(t.Context(), t.TempDir())
	secondWorkspace := t.TempDir()
	second := SubscribeWorkspaceEvents(t.Context(), secondWorkspace)
	name := t.Name()
	gens.Set(name, 7)

	broker.Publish(pubsub.CreatedEvent, Event{Type: EventChannelMessage, Name: name, Workspace: secondWorkspace, Generation: 7, ChannelMessage: "second"})
	broker.Publish(pubsub.CreatedEvent, Event{Type: EventChannelMessage, Name: name, Generation: 7, ChannelMessage: "unknown"})
	require.Equal(t, "second", requireScopedChannel(t, second, name).ChannelMessage)

	broker.Publish(pubsub.CreatedEvent, Event{Type: EventChannelMessage, Name: name, Workspace: secondWorkspace, Generation: 7, ChannelMessage: "cross"})
	select {
	case event := <-first:
		require.NotEqual(t, EventChannelMessage, event.Payload.Type, "cross-workspace channel message leaked")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWorkspaceChannelRoutingDropsStaleGeneration(t *testing.T) {
	workspace := t.TempDir()
	events := SubscribeWorkspaceEvents(t.Context(), workspace)
	name := t.Name()
	gens.Set(name, 2)
	broker.Publish(pubsub.CreatedEvent, Event{Type: EventChannelMessage, Name: name, Workspace: workspace, Generation: 1, ChannelMessage: "stale"})
	broker.Publish(pubsub.CreatedEvent, Event{Type: EventChannelMessage, Name: name, Workspace: workspace, Generation: 2, ChannelMessage: "current"})
	event := requireScopedChannel(t, events, name)
	require.Equal(t, uint64(2), event.Generation)
	require.Equal(t, "current", event.ChannelMessage)
}
