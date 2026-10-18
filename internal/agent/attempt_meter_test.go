package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func TestAdapterInvocationMeterCountsGenerateAndStreamAndStopsOnRecordFailure(t *testing.T) {
	t.Parallel()
	count := 0
	m := &attemptModel{LanguageModel: &finishStreamModel{text: "answer"}, prepare: func(c fantasy.Call) fantasy.Call { return c }, beforeCall: func(context.Context) error { count++; return nil }}
	_, err := m.Generate(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	_, err = m.Stream(t.Context(), fantasy.Call{})
	require.NoError(t, err)
	require.Equal(t, 2, count)
	blocked := &attemptModel{beforeCall: func(context.Context) error { return errors.New("meter persistence failed") }}
	_, err = blocked.Generate(t.Context(), fantasy.Call{})
	require.ErrorContains(t, err, "persistence failed")
	_, err = blocked.Stream(t.Context(), fantasy.Call{})
	require.ErrorContains(t, err, "persistence failed")
}
