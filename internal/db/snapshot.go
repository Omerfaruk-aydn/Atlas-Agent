package db

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// SnapshotSQLite writes a consistent copy of the SQLite database at src to
// dst with VACUUM INTO. The copy is made inside one read transaction, so it
// is safe while another process keeps writing; copying the database file
// directly is not. dst must not exist.
func SnapshotSQLite(ctx context.Context, src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("snapshot target %s already exists", dst)
	}
	conn, err := openPlain(src)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 15000"); err != nil {
		return fmt.Errorf("prepare snapshot: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("snapshot %s: %w", src, err)
	}
	return nil
}

// InspectSQLite runs SQLite's quick integrity check on path and returns the
// database's user_version, which apps use as their schema version.
func InspectSQLite(ctx context.Context, path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	conn, err := openPlain(path)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	var result string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return 0, fmt.Errorf("check %s: %w", path, err)
	}
	if result != "ok" {
		return 0, errors.New("integrity check failed: " + result)
	}
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}
