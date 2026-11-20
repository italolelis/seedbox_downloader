package storage

import "errors"

var (
	ErrDownloaded = errors.New("Download already completed")
	// ErrClaimLost reports an outcome recorded for a transfer this instance no
	// longer holds the claim on: its lease lapsed and another instance took over.
	ErrClaimLost = errors.New("claim on transfer was lost to another instance")
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
	// StatusFailed: the last local download attempt failed. The transfer is
	// claimed again, and the download retried, on the next poll.
	StatusFailed = "failed"
	// StatusMissing: the transfer, or its files, disappeared from the seedbox
	// before its local download finished.
	StatusMissing = "missing"
)

// IsLocal reports whether a transfer in status has been written to local disk in
// full. Only from Downloaded on is that so: a transfer that is pending,
// downloading, failed or missing may have a partial file on disk, and nothing
// should treat that file as the finished content.
func IsLocal(status string) bool {
	return status == StatusDownloaded || status == StatusImported || status == StatusCleaned
}

// DownloadRecord represents a record of a downloaded file.
type DownloadRecord struct {
	DownloadID   string
	DownloadedAt string
	Status       string
	LockedBy     string
}

type DownloadRepository interface {
	ClaimTransfer(transferID string) (bool, error)        // atomically claim a transfer
	UpdateTransferStatus(transferID, status string) error // move a transfer on; only its holder may
	RenewClaims() error                                   // extend this instance's leases on its claims
	ReleaseClaims() error                                 // give up every claim this instance holds
	ReleaseClaim(transferID string) error                 // hand one held transfer back
	ClaimWatches() ([]DownloadRecord, error)              // adopt watches no live instance holds
}
