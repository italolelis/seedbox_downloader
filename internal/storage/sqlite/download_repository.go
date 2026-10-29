package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/storage"
)

// DownloadRepository is the ledger. Its claims are leases: each one names the
// instance holding it and lapses unless that instance renews it, so a process
// that dies mid-download cannot hold its transfer forever.
type DownloadRepository struct {
	db *sql.DB
	// instanceID names this process in every claim it makes. Generated once, so
	// that RenewClaims can find this process's own claims again.
	instanceID string
	lease      time.Duration
}

// NewDownloadRepository returns a ledger whose claims last for lease unless
// renewed.
func NewDownloadRepository(dbConn *sql.DB, lease time.Duration) *DownloadRepository {
	return &DownloadRepository{db: dbConn, instanceID: storage.GenerateInstanceID(), lease: lease}
}

func (r *DownloadRepository) GetDownloads() ([]storage.DownloadRecord, error) {
	rows, err := r.db.Query(`SELECT transfer_id, downloaded_at, status, locked_by FROM downloads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []storage.DownloadRecord

	for rows.Next() {
		var record storage.DownloadRecord

		var lockedBy sql.NullString

		err := rows.Scan(&record.DownloadID, &record.DownloadedAt, &record.Status, &lockedBy)
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

// ClaimTransfer atomically sets status to 'downloading' and takes a lease on the
// transfer for this instance, if it is 'pending' or 'failed', or if it is
// 'downloading' under a lease that has lapsed -- the claim of a process that
// died. A claim without a lease predates leases, and has lapsed too.
func (r *DownloadRepository) ClaimTransfer(transferID string) (bool, error) {
	var status string

	err := r.db.QueryRow(`SELECT status FROM downloads WHERE transfer_id = ?`, transferID).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	switch status {
	case storage.StatusDownloaded, storage.StatusImported, storage.StatusCleaned:
		return false, storage.ErrDownloaded
	}

	// Now do the upsert/claim
	now := time.Now()

	rows, err := r.db.Exec(`
		INSERT INTO downloads (transfer_id, downloaded_at, status, locked_by, lease_until)
		VALUES (?, ?, 'downloading', ?, ?)
		ON CONFLICT(transfer_id) DO UPDATE SET
			status = 'downloading',
			locked_by = excluded.locked_by,
			lease_until = excluded.lease_until
		WHERE (downloads.status IN ('pending', 'failed') AND (downloads.locked_by IS NULL OR downloads.locked_by = ''))
			OR (downloads.status = 'downloading' AND (downloads.lease_until IS NULL OR downloads.lease_until < ?))
	`, transferID, now.Format(time.RFC3339), r.instanceID, now.Add(r.lease).UnixMilli(), now.UnixMilli())
	if err != nil {
		return false, err
	}

	affected, _ := rows.RowsAffected()

	return affected > 0, nil
}

// leasedStatuses are the statuses in which a transfer is held by one instance:
// downloading it, then watching it for import, then watching it seed. Only the
// holder moves such a transfer on.
const leasedStatuses = `'downloading', 'downloaded', 'imported'`

// RenewClaims extends the lease on every transfer this instance holds. Called on
// a heartbeat while the process is alive, it is what tells a live claim from a
// dead one.
func (r *DownloadRepository) RenewClaims() error {
	_, err := r.db.Exec(`UPDATE downloads SET lease_until = ? WHERE locked_by = ? AND status IN (`+leasedStatuses+`)`,
		time.Now().Add(r.lease).UnixMilli(), r.instanceID)
	if err != nil {
		return fmt.Errorf("failed to renew claims: %w", err)
	}

	return nil
}

// ReleaseClaims gives up every claim this instance holds, keeping each
// transfer's status, so that whoever starts next takes the work over at once
// rather than a lease later. Called on a graceful stop.
func (r *DownloadRepository) ReleaseClaims() error {
	_, err := r.db.Exec(`UPDATE downloads SET locked_by = NULL, lease_until = NULL WHERE locked_by = ?`, r.instanceID)
	if err != nil {
		return fmt.Errorf("failed to release claims: %w", err)
	}

	return nil
}

// ClaimWatches claims every transfer awaiting import or still seeding that no
// live instance holds -- its holder died or stopped, or it was recorded before
// watches were claimed -- and returns them. Claimed in one statement, so two
// instances adopting at once never both get the same transfer.
func (r *DownloadRepository) ClaimWatches() ([]storage.DownloadRecord, error) {
	now := time.Now()

	rows, err := r.db.Query(`
		UPDATE downloads SET locked_by = ?, lease_until = ?
		WHERE status IN ('downloaded', 'imported')
			AND (locked_by IS NULL OR locked_by = '' OR lease_until IS NULL OR lease_until < ?)
		RETURNING transfer_id, status
	`, r.instanceID, now.Add(r.lease).UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("failed to claim watches: %w", err)
	}
	defer rows.Close()

	var claimed []storage.DownloadRecord

	for rows.Next() {
		record := storage.DownloadRecord{LockedBy: r.instanceID}
		if err := rows.Scan(&record.DownloadID, &record.Status); err != nil {
			return nil, fmt.Errorf("failed to claim watches: %w", err)
		}

		claimed = append(claimed, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to claim watches: %w", err)
	}

	return claimed, nil
}

// UpdateTransferStatus moves a transfer to status.
//
// A transfer in a leased status is moved on only by the instance holding it. One
// that stalled past its lease has been taken over, and what it has to record is
// stale: it gets ErrClaimLost and the ledger is left to the current holder.
//
// Moving into a leased status keeps the claim, because the holder goes on to
// watch the transfer; it is renewed there and then. Any other status -- cleaned,
// failed, missing -- releases it.
func (r *DownloadRepository) UpdateTransferStatus(transferID, status string) error {
	res, err := r.db.Exec(`
		UPDATE downloads SET
			status = ?1,
			locked_by = CASE WHEN ?1 IN (`+leasedStatuses+`) THEN locked_by END,
			lease_until = CASE WHEN ?1 IN (`+leasedStatuses+`) THEN ?2 END
		WHERE transfer_id = ?3 AND (status NOT IN (`+leasedStatuses+`) OR locked_by = ?4)
	`, status, time.Now().Add(r.lease).UnixMilli(), transferID, r.instanceID)
	if err != nil {
		return fmt.Errorf("failed to update status of transfer %s: %w", transferID, err)
	}

	if affected, err := res.RowsAffected(); err != nil || affected > 0 {
		return nil
	}

	var current string

	err = r.db.QueryRow(`SELECT status FROM downloads WHERE transfer_id = ?`, transferID).Scan(&current)
	if err == nil && (current == "downloading" || current == storage.StatusDownloaded || current == storage.StatusImported) {
		return fmt.Errorf("cannot record %s for transfer %s: %w", status, transferID, storage.ErrClaimLost)
	}

	return nil
}
