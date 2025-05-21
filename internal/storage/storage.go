package storage

// DownloadRecord represents a record of a downloaded file.
type DownloadRecord struct {
	TorrentID    string
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
	TrackDownload(torrentID, filePath string) error
	ClaimDownload(torrentID, instanceID string) (bool, error) // atomically claim a download
	UpdateDownloadStatus(torrentID, status string) error      // update status after download
}
