package dots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestCreatePersistsAndWritesPersona(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	require.NoError(t, err)

	d, err := s.Create(Input{
		Name:         ptr("Selin"),
		Role:         ptr("Araştırma"),
		Shape:        ptr("bloom"),
		Color:        ptr("#3b7cf2"),
		SystemPrompt: ptr("Her sabah rakip fiyatlarını kontrol et."),
		Rules:        &Rules{AlwaysAsk: []string{"  e-posta göndermeden önce ", ""}},
	})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(d.ID, "dot_"))
	require.Equal(t, filepath.Join(dir, d.ID), d.Workspace)
	require.Equal(t, []string{"e-posta göndermeden önce"}, d.Rules.AlwaysAsk)

	persona, err := os.ReadFile(filepath.Join(d.Workspace, PersonaFile))
	require.NoError(t, err)
	require.Contains(t, string(persona), "# You are Selin")
	require.Contains(t, string(persona), "Her sabah rakip fiyatlarını kontrol et.")
	require.Contains(t, string(persona), "stop and ask your owner")

	reopened, err := Open(dir)
	require.NoError(t, err)
	require.Len(t, reopened.List(), 1)
	require.Equal(t, "Selin", reopened.List()[0].Name)
}

func TestCreateValidates(t *testing.T) {
	t.Parallel()
	s, err := Open(t.TempDir())
	require.NoError(t, err)

	_, err = s.Create(Input{Name: ptr("  ")})
	require.Error(t, err)
	_, err = s.Create(Input{Name: ptr("Ece"), Shape: ptr("triangle")})
	require.Error(t, err)
	_, err = s.Create(Input{Name: ptr("Ece"), Personality: &Personality{Warmth: 101}})
	require.Error(t, err)
	require.Empty(t, s.List())
}

func TestFullAccessExpires(t *testing.T) {
	t.Parallel()
	s, err := Open(t.TempDir())
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	d, err := s.Create(Input{Name: ptr("Deniz")})
	require.NoError(t, err)
	d, err = s.SetFullAccess(d.ID, time.Hour, false)
	require.NoError(t, err)
	require.True(t, d.FullAccess.Active(now))

	persona, err := os.ReadFile(filepath.Join(d.Workspace, PersonaFile))
	require.NoError(t, err)
	require.Contains(t, string(persona), "full access")

	now = now.Add(2 * time.Hour)
	require.NoError(t, s.ExpireFullAccess())
	got, err := s.Get(d.ID)
	require.NoError(t, err)
	require.False(t, got.FullAccess.Active(now))
	persona, err = os.ReadFile(filepath.Join(d.Workspace, PersonaFile))
	require.NoError(t, err)
	require.Contains(t, string(persona), "stop and ask your owner")
}

func TestUpdateAndDelete(t *testing.T) {
	t.Parallel()
	s, err := Open(t.TempDir())
	require.NoError(t, err)
	d, err := s.Create(Input{Name: ptr("Ece")})
	require.NoError(t, err)

	d, err = s.Update(d.ID, Input{Personality: &Personality{Warmth: 90, Humor: 80, Formality: 10, Proactivity: 90}, SessionID: ptr("s1")})
	require.NoError(t, err)
	require.Equal(t, "s1", d.SessionID)
	require.Equal(t, "Ece", d.Name)

	_, err = s.Update("missing", Input{})
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.Delete(d.ID))
	require.Empty(t, s.List())
	_, err = os.Stat(d.Workspace)
	require.NoError(t, err, "the workspace stays on disk")
}

func TestAccessLog(t *testing.T) {
	t.Parallel()
	s, err := Open(t.TempDir())
	require.NoError(t, err)
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	name := "Selin"
	d, err := s.Create(Input{Name: &name})
	require.NoError(t, err)

	d, err = s.SetFullAccess(d.ID, time.Hour, false)
	require.NoError(t, err)
	require.Len(t, d.AccessLog, 1)
	require.True(t, d.AccessLog[0].To.IsZero())

	now = now.Add(10 * time.Minute)
	d, err = s.SetFullAccess(d.ID, 0, false)
	require.NoError(t, err)
	require.Equal(t, "revoked", d.AccessLog[0].End)
	require.Equal(t, now, d.AccessLog[0].To)

	_, err = s.SetFullAccess(d.ID, time.Minute, false)
	require.NoError(t, err)
	now = now.Add(5 * time.Minute)
	require.NoError(t, s.ExpireFullAccess())
	d, err = s.Get(d.ID)
	require.NoError(t, err)
	require.Len(t, d.AccessLog, 2)
	require.Equal(t, "expired", d.AccessLog[1].End)
	require.Equal(t, d.AccessLog[1].Planned, d.AccessLog[1].To)

	skills := []string{"pdf", " pdf ", ""}
	d, err = s.Update(d.ID, Input{Skills: &skills})
	require.NoError(t, err)
	require.Equal(t, []string{"pdf"}, d.Skills)
	raw, err := os.ReadFile(filepath.Join(d.Workspace, PersonaFile))
	require.NoError(t, err)
	require.Contains(t, string(raw), "- pdf")
}
