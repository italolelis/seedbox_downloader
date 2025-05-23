package sqlite

import (
	"database/sql"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type DownloadRepository struct {
	db *sql.DB
}

func NewDownloadRepository(dbConn *sql.DB) *DownloadRepository {
	return &DownloadRepository{db: dbConn}
}

func (r *DownloadRepository) GetDownloads() ([]storage.DownloadRecord, error) {
	rows, err := r.db.Query(`SELECT download_id, file_path, downloaded_at, status, locked_by FROM downloads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []storage.DownloadRecord

	for rows.Next() {
		var record storage.DownloadRecord

		var lockedBy sql.NullString

		err := rows.Scan(&record.DownloadID, &record.FilePath, &record.DownloadedAt, &record.Status, &lockedBy)
		if err != nil {
			return nil, err
		}

		record.LockedBy = ""
		if lockedBy.Valid {
			record.LockedBy = lockedBy.String
		}

		downloads = append(downloads, record)
	}

	return downloads, nil
}

// ClaimDownload atomically sets status to 'downloading' and locked_by to instanceID if status is 'pending' or 'failed'.
func (r *DownloadRepository) ClaimDownload(downloadID, torrentID, targetPath, instanceID string) (bool, error) {
	var status string

	err := r.db.QueryRow(`SELECT status FROM downloads WHERE download_id = ?`, downloadID).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	if status == "downloaded" {
		return false, storage.ErrDownloaded
	}

	// Now do the upsert/claim
	rows, err := r.db.Exec(`
		INSERT INTO downloads (download_id, torrent_id, file_path, downloaded_at, status, locked_by)
		VALUES (?, ?, ?, ?, 'downloading', ?)
		ON CONFLICT(download_id) DO UPDATE SET
			status = 'downloading',
			locked_by = excluded.locked_by
		WHERE downloads.status IN ('pending', 'failed') AND (downloads.locked_by IS NULL OR downloads.locked_by = '')
	`, downloadID, torrentID, targetPath, time.Now().Format(time.RFC3339), instanceID)
	if err != nil {
		return false, err
	}

	affected, _ := rows.RowsAffected()

	return affected > 0, nil
}

// UpdateDownloadStatus sets the status for a download.
func (r *DownloadRepository) UpdateDownloadStatus(downloadID, status string) error {
	_, err := r.db.Exec(`UPDATE downloads SET status = ?, locked_by = NULL WHERE download_id = ?`, status, downloadID)

	return err
}
