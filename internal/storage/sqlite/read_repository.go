package sqlite

import (
	"database/sql"

	"github.com/italolelis/seedbox_downloader/internal/storage"
)

type DownloadReadRepository struct {
	db *sql.DB
}

func NewDownloadReadRepository(dbConn *sql.DB) *DownloadReadRepository {
	return &DownloadReadRepository{db: dbConn}
}

func (r *DownloadReadRepository) GetDownloads() ([]storage.DownloadRecord, error) {
	rows, err := r.db.Query(`SELECT download_id, file_path, downloaded_at, status, locked_by FROM downloads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []storage.DownloadRecord

	for rows.Next() {
		var record storage.DownloadRecord
		if err := rows.Scan(&record.DownloadID, &record.FilePath, &record.DownloadedAt, &record.Status, &record.LockedBy); err != nil {
			return nil, err
		}

		downloads = append(downloads, record)
	}

	return downloads, nil
}

// GetPendingDownloads returns downloads that are pending and not locked, up to a limit.
func (r *DownloadReadRepository) GetPendingDownloads(limit int) ([]storage.DownloadRecord, error) {
	rows, err := r.db.Query(
		`SELECT 
			download_id, 
			file_path, 
			downloaded_at, 
			status, 
			locked_by 
		FROM downloads
		WHERE status = 'pending' 
		AND (locked_by IS NULL OR locked_by = '') 
		LIMIT ?`, limit)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var downloads []storage.DownloadRecord

	for rows.Next() {
		var record storage.DownloadRecord
		if err := rows.Scan(&record.DownloadID, &record.FilePath, &record.DownloadedAt, &record.Status, &record.LockedBy); err != nil {
			return nil, err
		}

		downloads = append(downloads, record)
	}

	return downloads, nil
}
