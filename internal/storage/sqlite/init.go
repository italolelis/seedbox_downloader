package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

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

	if err := addLeaseColumn(ctx, db); err != nil {
		db.Close()

		return nil, err
	}

	return db, nil
}

// addLeaseColumn brings a ledger created before claims had leases up to date.
// SQLite has no ADD COLUMN IF NOT EXISTS, so the ALTER is attempted and an
// existing column accepted -- which also settles two instances upgrading the
// same ledger at once, where checking first would let both try to add it.
func addLeaseColumn(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `ALTER TABLE downloads ADD COLUMN lease_until INTEGER`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("failed to add lease_until to the ledger: %w", err)
	}

	return nil
}
