package agent

import (
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestIdleCompactionDueRequiresIdleLargeRootSession(t *testing.T) {
	t.Parallel()
	now := time.Unix(10_000, 0)
	base := session.Session{ID: "root", PromptTokens: 25_000, CompletionTokens: 7_000, UpdatedAt: now.Add(-time.Hour).Unix()}
	require.True(t, idleCompactionDue(base, now, 15*time.Minute, 32_000))

	cases := map[string]session.Session{
		"child":       {ParentSessionID: "parent", PromptTokens: 32_000, UpdatedAt: base.UpdatedAt},
		"small":       {PromptTokens: 31_999, UpdatedAt: base.UpdatedAt},
		"recent":      {PromptTokens: 32_000, UpdatedAt: now.Add(-time.Minute).Unix()},
		"unknownTime": {PromptTokens: 32_000},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.False(t, idleCompactionDue(s, now, 15*time.Minute, 32_000))
		})
	}
}
