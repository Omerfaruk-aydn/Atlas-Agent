package jsonstrict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStrictCompleteRequests(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{"action":"pause"}{}`, `{"action":"pause"}garbage`, `{"action":"pause","action":"resume"}`, `{"Action":"pause"}`, `{"nested":{"id":1,"id":2}}`} {
		require.Error(t, Validate(t.Context(), []byte(input)))
	}
	require.NoError(t, Validate(t.Context(), []byte(`{"action":"pause","array":[1,{"ok":true}]} `)))
}
