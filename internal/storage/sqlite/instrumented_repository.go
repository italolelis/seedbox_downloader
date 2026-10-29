package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/telemetry"
)

// InstrumentedDownloadRepository wraps DownloadRepository with telemetry.
type InstrumentedDownloadRepository struct {
	repo      *DownloadRepository
	telemetry *telemetry.Telemetry
}

// NewInstrumentedDownloadRepository creates a new instrumented download repository.
func NewInstrumentedDownloadRepository(dbConn *sql.DB, lease time.Duration, tel *telemetry.Telemetry) *InstrumentedDownloadRepository {
	return &InstrumentedDownloadRepository{
		repo:      NewDownloadRepository(dbConn, lease),
		telemetry: tel,
	}
}

// GetDownloads retrieves all downloads with telemetry.
func (r *InstrumentedDownloadRepository) GetDownloads() ([]storage.DownloadRecord, error) {
	var result []storage.DownloadRecord

	var err error

	instrumentedErr := r.telemetry.InstrumentDBOperation(context.Background(), "get_downloads", func(ctx context.Context) error {
		result, err = r.repo.GetDownloads()

		return err
	})

	if instrumentedErr != nil {
		return nil, instrumentedErr
	}

	return result, nil
}

// ClaimTransfer claims a transfer with telemetry.
func (r *InstrumentedDownloadRepository) ClaimTransfer(transferID string) (bool, error) {
	var result bool

	var err error

	instrumentedErr := r.telemetry.InstrumentDBOperation(context.Background(), "claim_transfer", func(ctx context.Context) error {
		result, err = r.repo.ClaimTransfer(transferID)

		return err
	})

	if instrumentedErr != nil {
		return false, instrumentedErr
	}

	return result, nil
}

// UpdateTransferStatus updates transfer status with telemetry.
func (r *InstrumentedDownloadRepository) UpdateTransferStatus(transferID, status string) error {
	return r.telemetry.InstrumentDBOperation(context.Background(), "update_transfer_status", func(ctx context.Context) error {
		return r.repo.UpdateTransferStatus(transferID, status)
	})
}

// RenewClaims renews this instance's claims with telemetry.
func (r *InstrumentedDownloadRepository) RenewClaims() error {
	return r.telemetry.InstrumentDBOperation(context.Background(), "renew_claims", func(ctx context.Context) error {
		return r.repo.RenewClaims()
	})
}

// ReleaseClaims releases this instance's claims with telemetry.
func (r *InstrumentedDownloadRepository) ReleaseClaims() error {
	return r.telemetry.InstrumentDBOperation(context.Background(), "release_claims", func(ctx context.Context) error {
		return r.repo.ReleaseClaims()
	})
}

// ClaimWatches adopts unheld watches with telemetry.
func (r *InstrumentedDownloadRepository) ClaimWatches() ([]storage.DownloadRecord, error) {
	var result []storage.DownloadRecord

	err := r.telemetry.InstrumentDBOperation(context.Background(), "claim_watches", func(ctx context.Context) error {
		var err error

		result, err = r.repo.ClaimWatches()

		return err
	})

	return result, err
}
