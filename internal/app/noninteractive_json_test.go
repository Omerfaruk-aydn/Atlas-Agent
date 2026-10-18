package app

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/stretchr/testify/require"
)

func TestMeasuredNonInteractiveOutputDistinguishesInvocationsFromResponses(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	n := int64(3)
	require.NoError(t, printNonInteractiveMeasured(&out, session.Session{ID: "s"}, "answer", &n))
	var got NonInteractiveResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, int64(3), *got.ProviderInvocations)
	require.Zero(t, got.ProviderSteps)
	require.Contains(t, got.ProviderInvocationBasis, "adapter-internal HTTP retries")
}

func TestPrintNonInteractiveResult(t *testing.T) {
	var out bytes.Buffer
	sess := session.Session{
		ID:               "sess-1",
		PromptTokens:     120,
		CompletionTokens: 45,
		Cost:             0.0031,
	}
	require.NoError(t, printNonInteractiveResult(&out, sess, "  the answer\n"))

	var got NonInteractiveResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, NonInteractiveResult{
		SessionID:        "sess-1",
		Response:         "the answer",
		PromptTokens:     120,
		CompletionTokens: 45,
		Cost:             0.0031,
	}, got)
}

// The streamed form ends with a newline the terminal wants; a JSON consumer
// does not.
func TestPrintNonInteractiveResultTrimsTheResponse(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printNonInteractiveResult(&out, session.Session{ID: "s"}, "\n\nanswer\n\n"))

	var got NonInteractiveResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	require.Equal(t, "answer", got.Response)
}

// HTML escaping would mangle any response containing <, > or & -- which is
// most code.
func TestPrintNonInteractiveResultDoesNotEscapeHTML(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printNonInteractiveResult(&out, session.Session{ID: "s"}, "if a < b && c > d"))
	require.Contains(t, out.String(), "if a < b && c > d")
	require.NotContains(t, out.String(), "\\u003c")
}

// One document per run: a caller parsing stdout must not have to reassemble
// a stream.
func TestPrintNonInteractiveResultIsASingleJSONDocument(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printNonInteractiveResult(&out, session.Session{ID: "s"}, "line one\nline two"))

	dec := json.NewDecoder(&out)
	var first NonInteractiveResult
	require.NoError(t, dec.Decode(&first))
	require.Equal(t, "line one\nline two", first.Response)
	require.False(t, dec.More(), "there must be exactly one document")
}
