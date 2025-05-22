package storage

import "errors"

var (
	ErrDownloaded = errors.New("Download already completed")
)

// DownloadRecord represents a record of a downloaded file.
type DownloadRecord struct {
	DownloadID   string
	FilePath     string
	DownloadedAt string
	Status       string
	LockedBy     string
}

type DownloadRepository interface {
	GetDownloads() ([]DownloadRecord, error)                                          // get all downloads
	ClaimDownload(downloadID, torrentID, targetPath, instanceID string) (bool, error) // atomically claim a download
	UpdateDownloadStatus(downloadID, status string) error                             // update status after download
}
