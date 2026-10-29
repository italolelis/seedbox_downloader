package storage

import "errors"

var (
	ErrDownloaded = errors.New("Download already completed")
)

// The statuses a transfer moves through after it is Downloaded. Each one is
// written before the step it leads into, so after a restart the ledger alone says
// which watch a transfer still needs.
const (
	// StatusDownloaded: on local disk, waiting for an *arr app to import it.
	StatusDownloaded = "downloaded"
	// StatusImported: imported, and still on the seedbox until it has seeded
	// enough to be removed.
	StatusImported = "imported"
	// StatusCleaned: removed from the seedbox. Terminal.
	StatusCleaned = "cleaned"
)

// DownloadRecord represents a record of a downloaded file.
type DownloadRecord struct {
	DownloadID   string
	DownloadedAt string
	Status       string
	LockedBy     string
}

type DownloadRepository interface {
	GetDownloads() ([]DownloadRecord, error)              // get all downloads
	ClaimTransfer(transferID string) (bool, error)        // atomically claim a transfer
	UpdateTransferStatus(transferID, status string) error // update status after download
}
