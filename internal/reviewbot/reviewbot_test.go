package reviewbot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishableRequiresSecondPassEvidence(t *testing.T) {
	t.Parallel()
	valid := Finding{Path: "internal/a.go", Line: 42, Severity: "high", Title: "Lost update", Body: "Concurrent writes lose data.", Evidence: "The lock is released before Store.", Confirmed: true}
	findings, err := Publishable([]Finding{
		valid,
		valid,
		{Path: "a.go", Line: 1, Severity: "high", Title: "Unverified", Body: "Maybe", Confirmed: true},
		{Path: "a.go", Line: 1, Severity: "low", Title: "Style", Body: "Rename.", Evidence: "Readability.", Confirmed: true},
	}, "medium")
	require.NoError(t, err)
	require.Equal(t, []Finding{valid}, findings)
	_, err = Publishable(nil, "fatal")
	require.Error(t, err)
}

func TestNewFindingsIsIdempotent(t *testing.T) {
	t.Parallel()
	finding := Finding{Path: "a.go", Line: 7, Severity: "high", Title: "Nil dereference", Body: "Crash.", Evidence: "nil is returned.", Confirmed: true}
	comment := Comment(finding)
	require.Contains(t, comment, Marker+Fingerprint(finding))
	require.Empty(t, NewFindings([]Finding{finding}, []string{comment}))
	require.Len(t, NewFindings([]Finding{finding}, []string{"unrelated comment"}), 1)
}

func TestDecodeFindings(t *testing.T) {
	t.Parallel()
	findings, err := DecodeFindings(`{"findings":[{"path":"a.go","line":2,"severity":"high","title":"Bug","body":"Impact","evidence":"Code","confirmed":true}]}`)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	_, err = DecodeFindings(`{"findings":[],"unexpected":true}`)
	require.Error(t, err)
}
