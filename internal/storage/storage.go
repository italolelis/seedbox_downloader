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
	GetDownloads() ([]DownloadRecord, error)              // get all downloads
	ClaimTransfer(transferID string) (bool, error)        // atomically claim a transfer
	UpdateTransferStatus(transferID, status string) error // update status after download
}
