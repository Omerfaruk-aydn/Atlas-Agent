package rehearsal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRehearsalPreservesFixtureAcrossMigration(t *testing.T) {
	t.Parallel()
	result, err := SQLite(t.Context(), "CREATE TABLE users(id INTEGER); INSERT INTO users VALUES(1);", "ALTER TABLE users ADD COLUMN name TEXT DEFAULT 'fixture';", "ALTER TABLE users DROP COLUMN name;", []Assertion{{SQL: "SELECT COUNT(*) FROM users", Expected: "1"}}, []Assertion{{SQL: "SELECT name FROM users", Expected: "fixture"}})
	require.NoError(t, err)
	require.True(t, result.Passed)
	require.True(t, result.RollbackChecked)
}

func TestRehearsalRejectsHostDatabaseAttachment(t *testing.T) {
	t.Parallel()
	_, err := SQLite(t.Context(), "", "ATTACH DATABASE 'outside.sqlite' AS other;", "", nil, []Assertion{{SQL: "SELECT 1", Expected: "1"}})
	require.ErrorContains(t, err, "external-file")
}
