package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/cenkalti/backoff/v5"
	// Import the SQLite driver.
	_ "github.com/mattn/go-sqlite3"
)

// InitDB initializes the SQLite database and creates the downloads table if it doesn't exist.
func InitDB(ctx context.Context, dbPath string, maxOpenConns, maxIdleConns int) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Configure connection pool limits
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)

	// Validate database connectivity with exponential backoff retry
	retryCount := 0
	_, err = backoff.Retry(ctx, func() (struct{}, error) {
		if retryCount > 0 {
			slog.DebugContext(ctx, "retrying database ping", "attempt", retryCount+1)
		}

		retryCount++

		return struct{}{}, db.PingContext(ctx)
	}, backoff.WithMaxTries(3))

	if err != nil {
		db.Close()

		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS downloads (
		transfer_id TEXT UNIQUE,
		downloaded_at DATETIME,
		status TEXT DEFAULT 'pending',
		locked_by TEXT
	)`)

	if err != nil {
		db.Close()

		return nil, err
	}

	if err := addColumnIfMissing(db, "downloads", "lease_until", "INTEGER"); err != nil {
		db.Close()

		return nil, err
	}

	return db, nil
}

// addColumnIfMissing brings a ledger created by an older version up to date.
// SQLite has no ADD COLUMN IF NOT EXISTS, so the table is inspected first.
func addColumnIfMissing(db *sql.DB, table, column, columnType string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("failed to inspect table %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("failed to inspect table %s: %w", table, err)
		}

		if name == column {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to inspect table %s: %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, columnType)); err != nil {
		return fmt.Errorf("failed to add column %s.%s: %w", table, column, err)
	}

	return nil
}
