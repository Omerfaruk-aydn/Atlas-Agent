package interaction

import (
	"context"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/activity"
	"github.com/stretchr/testify/require"
)

type gateRenderer struct{}

func (gateRenderer) Render(activity.Event) {}
func (gateRenderer) Close()                {}

func TestControlWaitsWhileTheRunAwaitsTheUser(t *testing.T) {
	m := activity.New(gateRenderer{})
	defer m.Close()
	ctx, end := activity.StartFlow(context.Background(), "gate")
	defer end()
	m.Begin(ctx, activity.Event{Session: "gate", Resource: "desktop"})()
	pending := activity.AwaitPrompt(ctx, activity.Prompt{Kind: activity.KindPermission, ID: "p", Permission: activity.PromptPermission{Decisions: []activity.PermissionDecision{activity.DecisionDeny}}}, func(activity.PromptResponse) bool { return true })
	c := New()
	for _, observe := range []bool{false, true} {
		short, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
		_, err := c.Acquire(short, "gate", "desktop", observe)
		cancel()
		require.ErrorIs(t, err, context.DeadlineExceeded, "Neither input nor capture runs before the answer (observe=%t)", observe)
	}
	acquired := make(chan error, 1)
	go func() {
		release, err := c.Acquire(ctx, "gate", "desktop")
		if release != nil {
			release()
		}
		acquired <- err
	}()
	select {
	case <-acquired:
		t.Fatal("Control was taken while waiting")
	case <-time.After(50 * time.Millisecond):
	}
	pending.Done(activity.OutcomeDenied)
	require.NoError(t, <-acquired)
}
