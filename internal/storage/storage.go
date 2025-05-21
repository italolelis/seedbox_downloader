package storage

// DownloadRecord represents a record of a downloaded file.
type DownloadRecord struct {
	DownloadID   string
	FilePath     string
	DownloadedAt string
	Status       string
	LockedBy     string
}

// DownloadReadRepository interface remains here.
type DownloadReadRepository interface {
	GetDownloads() ([]DownloadRecord, error)
	GetPendingDownloads(limit int) ([]DownloadRecord, error) // new method for pending/available downloads
}

type DownloadWriteRepository interface {
	TrackDownload(downloadID, filePath string) error
	ClaimDownload(downloadID, instanceID string) (bool, error) // atomically claim a download
	UpdateDownloadStatus(downloadID, status string) error      // update status after download
}
