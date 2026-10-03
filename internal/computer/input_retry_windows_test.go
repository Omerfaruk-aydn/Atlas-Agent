//go:build windows

package computer

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPartialInputRetriesOnlyUnacceptedTail(t *testing.T) {
	t.Parallel()
	inputs := []winInput{{Type: 10}, {Type: 20}, {Type: 30}}
	attempt := 0
	err := sendInputBatch(inputs, func(batch []winInput) (int, error) {
		attempt++
		if attempt == 1 {
			return 2, errors.New("partial")
		}
		require.Equal(t, []winInput{{Type: 30}}, batch)
		return len(batch), nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempt)
}

func TestUncertainInputDoesNotRepeatAcceptedBatch(t *testing.T) {
	t.Parallel()
	attempt := 0
	err := sendInputBatch([]winInput{{Type: 10}}, func(batch []winInput) (int, error) { attempt++; return len(batch), errors.New("completion uncertain") })
	require.Error(t, err)
	require.Equal(t, 1, attempt)
}
