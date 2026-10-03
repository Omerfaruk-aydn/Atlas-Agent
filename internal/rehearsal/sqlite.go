// Package rehearsal executes migrations against isolated disposable databases.
package rehearsal

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

var externalSQL = regexp.MustCompile(`(?i)\b(attach|detach|vacuum|pragma|load_extension|readfile|writefile)\b`)

type Assertion struct {
	SQL      string `json:"sql"`
	Expected string `json:"expected"`
}

type Result struct {
	Engine          string `json:"engine"`
	BeforeChecks    int    `json:"before_checks"`
	AfterChecks     int    `json:"after_checks"`
	RollbackChecked bool   `json:"rollback_checked"`
	Passed          bool   `json:"passed"`
}

// SQLite rejects external-file SQL and uses one in-memory connection.
func SQLite(ctx context.Context, seed, migration, rollback string, before, after []Assertion) (Result, error) {
	result := Result{Engine: "sqlite in-memory"}
	if len(seed) > 512*1024 || len(migration) > 512*1024 || len(rollback) > 512*1024 || migration == "" || len(before) > 16 || len(after) < 1 || len(after) > 16 {
		return result, fmt.Errorf("invalid migration scripts or assertion bounds")
	}
	for _, script := range []string{seed, migration, rollback} {
		if externalSQL.MatchString(script) {
			return result, fmt.Errorf("external-file operations, PRAGMA and extensions are unsupported in migration rehearsal")
		}
	}
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return result, err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, seed); err != nil {
		return result, fmt.Errorf("seed: %w", err)
	}
	checkOne := func(assertion Assertion) error {
		if len(assertion.SQL) > 16384 || len(assertion.Expected) > 4096 || externalSQL.MatchString(assertion.SQL) || !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(assertion.SQL)), "SELECT ") || strings.Contains(strings.TrimSuffix(strings.TrimSpace(assertion.SQL), ";"), ";") {
			return fmt.Errorf("invalid or unsupported assertion")
		}
		rows, err := database.QueryContext(ctx, assertion.SQL)
		if err != nil {
			return err
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		if len(columns) != 1 || !rows.Next() {
			return fmt.Errorf("assertion must return one row and one column")
		}
		var actual any
		if err := rows.Scan(&actual); err != nil {
			return err
		}
		if rows.Next() {
			return fmt.Errorf("assertion returns more than one row")
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if fmt.Sprint(actual) != assertion.Expected {
			return fmt.Errorf("migration assertion failed")
		}
		return nil
	}
	check := func(assertions []Assertion) error {
		for _, assertion := range assertions {
			if err := checkOne(assertion); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(before); err != nil {
		return result, fmt.Errorf("before migration: %w", err)
	}
	result.BeforeChecks = len(before)
	if _, err := database.ExecContext(ctx, migration); err != nil {
		return result, fmt.Errorf("migration: %w", err)
	}
	if err := check(after); err != nil {
		return result, fmt.Errorf("after migration: %w", err)
	}
	result.AfterChecks = len(after)
	if rollback != "" {
		if _, err := database.ExecContext(ctx, rollback); err != nil {
			return result, fmt.Errorf("rollback: %w", err)
		}
		if len(before) == 0 {
			return result, fmt.Errorf("rollback requires before assertions")
		}
		if err := check(before); err != nil {
			return result, fmt.Errorf("after rollback: %w", err)
		}
		result.RollbackChecked = true
	}
	result.Passed = true
	return result, nil
}
