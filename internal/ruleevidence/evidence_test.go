package ruleevidence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHintsNeedRealEvidenceAndUsefulLife(t *testing.T) {
	t.Parallel()
	now := time.Now()
	o := Origin{Session: "s", Turn: 1, Signature: "error-class", At: now.UnixMilli()}
	require.False(t, Expired(o, Usage{}, now))
	require.True(t, Expired(Origin{}, Usage{}, now))
	require.True(t, Expired(o, Usage{Failed: 3}, now))
	require.True(t, Expired(o, Usage{Offered: 10}, now))
	require.True(t, Expired(o, Usage{}, now.Add(31*24*time.Hour)))
}
