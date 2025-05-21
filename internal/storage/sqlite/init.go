package sqlite

import (
	"database/sql"

	// Import the SQLite driver.
	_ "github.com/mattn/go-sqlite3"
)

const dbFile = "downloads.db"

// InitDB initializes the SQLite database and creates the downloads table if it doesn't exist.
func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS downloads (
		id INTEGER PRIMARY KEY,
		torrent_id TEXT UNIQUE,
		file_path TEXT,
		downloaded_at DATETIME,
		status TEXT DEFAULT 'pending',
		locked_by TEXT
	)`)

	if err != nil {
		return nil, err
	}

	return db, nil
}
