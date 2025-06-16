package sqlite

import (
	"database/sql"

	// Import the SQLite driver.
	_ "github.com/mattn/go-sqlite3"
)

// InitDB initializes the SQLite database and creates the downloads table if it doesn't exist.
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS downloads (
		transfer_id TEXT UNIQUE,
		downloaded_at DATETIME,
		status TEXT DEFAULT 'pending',
		locked_by TEXT
	)`)

	if err != nil {
		return nil, err
	}

	return db, nil
}
