package catwalk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetirementScheduleAndDefaults(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	p := Provider{ID: "host-a", DefaultLargeModelID: "old", Models: []Model{
		{ID: "old", Lifecycle: &ModelLifecycle{Status: "deprecated", RetirementDate: "2026-11-16", Replacement: "new", SourceURL: "https://example.com/models", VerifiedAt: "2026-10-20"}},
		{ID: "new"},
	}}
	before := p.SelectableAt(cutoff.Add(-time.Second))
	require.Len(t, before.Models, 2)
	require.Equal(t, "old", before.DefaultLargeModelID)
	after := p.SelectableAt(cutoff)
	require.Len(t, after.Models, 1)
	require.Equal(t, "new", after.DefaultLargeModelID)
	require.Len(t, p.Models, 2, "selection must not modify the source catalog")
	require.Equal(t, "old", p.DefaultLargeModelID)
	otherHost := Model{ID: "old"}
	require.False(t, otherHost.RetiredAt(cutoff), "retirement is provider-specific")
}

func TestUnverifiedAndAliasMetadataCannotRetireModels(t *testing.T) {
	t.Parallel()
	for _, lifecycle := range []*ModelLifecycle{
		nil,
		{Status: "retired"},
		{Status: "retired", SourceURL: "invalid", VerifiedAt: "2026-10-20"},
		{Status: "retired", SourceURL: "https://example.com", VerifiedAt: "invalid"},
		{Status: "deprecated", SourceURL: "https://example.com", VerifiedAt: "2026-10-20", RetirementDate: "invalid"},
		{Status: "alias", SourceURL: "https://example.com", VerifiedAt: "2026-10-20", RetirementDate: "2020-01-01"},
	} {
		require.False(t, (Model{Lifecycle: lifecycle}).RetiredAt(time.Now()))
	}
}

func TestRetirementReplacementCycleTerminates(t *testing.T) {
	t.Parallel()
	lifecycle := func(replacement string) *ModelLifecycle {
		return &ModelLifecycle{Status: "retired", Replacement: replacement, SourceURL: "https://example.com", VerifiedAt: "2026-10-20"}
	}
	p := Provider{DefaultLargeModelID: "a", Models: []Model{
		{ID: "a", Lifecycle: lifecycle("b")}, {ID: "b", Lifecycle: lifecycle("a")}, {ID: "usable"},
	}}
	p = p.SelectableAt(time.Now())
	require.Equal(t, "usable", p.DefaultLargeModelID)
	require.Len(t, p.Models, 1)
}
