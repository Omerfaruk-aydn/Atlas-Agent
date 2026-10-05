package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type pointerTestRenderer struct{}

func (pointerTestRenderer) Render(activity.Event) {}
func (pointerTestRenderer) Close()                {}

func TestSemanticInvokeFeedbackRequiresConfirmedOwnedTarget(t *testing.T) {
	manager := activity.New(pointerTestRenderer{})
	previous := activity.Default
	activity.Default = manager
	t.Cleanup(func() { activity.Default = previous; manager.Close() })
	for _, test := range []struct {
		name, owner          string
		confirmed, wantClick bool
	}{
		{name: "Confirmed target", owner: "owner", confirmed: true, wantClick: true},
		{name: "Unconfirmed target", owner: "owner"},
		{name: "Other owner", owner: "other", confirmed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.WithValue(t.Context(), SessionIDContextKey, "owner")
			finish := manager.Begin(ctx, activity.Event{Session: test.owner, Resource: "desktop", WindowID: "11", Point: true, X: 120, Y: 220})
			defer finish()
			backend := &efficientDesktopBackend{}
			backend.call = func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
				data, err := json.Marshal(map[string]bool{"action_sent": test.confirmed})
				return data, err
			}
			state := computerToolState{backend: backend, overlay: true}
			response, err := state.runAutomation(ctx, "invoke", ComputerParams{Automation: computer.AutomationRequest{WindowID: "11", Name: "Save"}})
			require.NoError(t, err)
			require.False(t, response.IsError)
			require.Equal(t, test.wantClick, manager.Snapshot().PointerKind == "click")
		})
	}
}
