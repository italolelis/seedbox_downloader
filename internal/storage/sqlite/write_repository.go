package sqlite

import (
	"database/sql"
	"time"
)

// DownloadWriteRepository implements storage.DownloadWriteRepository
// and stores download records in SQLite.
type DownloadWriteRepository struct {
	db *sql.DB
}

func NewDownloadWriteRepository(db *sql.DB) *DownloadWriteRepository {
	return &DownloadWriteRepository{db: db}
}

func (r *DownloadWriteRepository) TrackDownload(torrentID, filePath string) error {
	_, err := r.db.Exec(
		`INSERT INTO downloads (download_id, file_path, downloaded_at, status) VALUES (?, ?, ?, 'pending')`,
		torrentID, filePath, time.Now().Format(time.RFC3339),
	)

	return err
}

// ClaimDownload atomically sets status to 'downloading' and locked_by to instanceID if status is 'pending'.
func (r *DownloadWriteRepository) ClaimDownload(torrentID, instanceID string) (bool, error) {
	res, err := r.db.Exec(
		`UPDATE downloads SET status = 'downloading', locked_by = ? WHERE download_id = ? AND status = 'pending' AND (locked_by IS NULL OR locked_by = '')`,
		instanceID, torrentID,
	)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

// UpdateDownloadStatus sets the status for a torrent.
func (r *DownloadWriteRepository) UpdateDownloadStatus(torrentID, status string) error {
	_, err := r.db.Exec(`UPDATE downloads SET status = ? WHERE download_id = ?`, status, torrentID)

	return err
}
