package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLease is short so a lease can be let lapse inside a test.
const testLease = 100 * time.Millisecond

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := InitDB(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5, 2)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })

	return db
}

func newTestRepo(t *testing.T) *DownloadRepository {
	t.Helper()

	return NewDownloadRepository(newTestDB(t), testLease)
}

func TestClaimTransfer_FirstClaimSucceeds(t *testing.T) {
	repo := newTestRepo(t)

	claimed, err := repo.ClaimTransfer("100")
	require.NoError(t, err)
	assert.True(t, claimed)
}

func TestClaimTransfer_AlreadyClaimedIsNotClaimedAgain(t *testing.T) {
	repo := newTestRepo(t)

	claimed, err := repo.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	// A second instance polling the same shared account must not fetch it twice.
	claimed, err = repo.ClaimTransfer("100")
	require.NoError(t, err)
	assert.False(t, claimed, "a transfer already being downloaded must not be claimed again")
}

// This is what makes retry after a failure possible: marking a transfer failed
// has to release the claim, or a transfer that failed once is stuck forever --
// which is exactly what the wedge caused, since it never got this far.
func TestClaimTransfer_FailedTransferCanBeClaimedAgain(t *testing.T) {
	repo := newTestRepo(t)

	claimed, err := repo.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.UpdateTransferStatus("100", "failed"))

	claimed, err = repo.ClaimTransfer("100")
	require.NoError(t, err)
	assert.True(t, claimed, "a failed transfer must be eligible for retry on a later poll")
}

func TestClaimTransfer_DownloadedTransferIsNotClaimedAgain(t *testing.T) {
	repo := newTestRepo(t)

	claimed, err := repo.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, repo.UpdateTransferStatus("100", "downloaded"))

	claimed, err = repo.ClaimTransfer("100")
	assert.False(t, claimed)
	assert.ErrorIs(t, err, storage.ErrDownloaded,
		"an already-downloaded transfer must be reported as such, not silently skipped")
}

// Resuming after a restart starts from what the ledger holds, so reading it back
// has to work. It used to scan five destinations out of a four-column query and
// fail on the first row.
func TestGetDownloads_ReturnsEachTransferWithItsStatus(t *testing.T) {
	repo := newTestRepo(t)

	for _, id := range []string{"100", "200"} {
		claimed, err := repo.ClaimTransfer(id)
		require.NoError(t, err)
		require.True(t, claimed)
	}

	require.NoError(t, repo.UpdateTransferStatus("200", "downloaded"))

	records, err := repo.GetDownloads()
	require.NoError(t, err)

	statuses := map[string]string{}
	for _, r := range records {
		statuses[r.DownloadID] = r.Status
	}

	assert.Equal(t, map[string]string{"100": "downloading", "200": "downloaded"}, statuses)
}

// A transfer stays on the seedbox while it seeds, so it keeps turning up in every
// poll after it was imported. It must keep being recognised as downloaded, not
// mistaken for one another instance is still working on.
func TestClaimTransfer_EveryPostDownloadStatusIsReportedAsDownloaded(t *testing.T) {
	for _, status := range []string{storage.StatusDownloaded, storage.StatusImported, storage.StatusCleaned} {
		t.Run(status, func(t *testing.T) {
			repo := newTestRepo(t)

			_, err := repo.ClaimTransfer("100")
			require.NoError(t, err)
			require.NoError(t, repo.UpdateTransferStatus("100", status))

			claimed, err := repo.ClaimTransfer("100")
			assert.False(t, claimed)
			assert.ErrorIs(t, err, storage.ErrDownloaded)
		})
	}
}

// A process that dies mid-download never marks its transfer failed, so its claim
// used to hold forever and the transfer was never downloaded by anyone. A claim
// is now a lease: once it lapses, another instance -- or the same one restarted
// -- takes the transfer over.
func TestClaimTransfer_LapsedClaimIsTakenOver(t *testing.T) {
	db := newTestDB(t)
	crashed, survivor := NewDownloadRepository(db, testLease), NewDownloadRepository(db, testLease)

	claimed, err := crashed.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = survivor.ClaimTransfer("100")
	require.NoError(t, err)
	require.False(t, claimed, "a live claim must not be taken over")

	time.Sleep(2 * testLease)

	claimed, err = survivor.ClaimTransfer("100")
	require.NoError(t, err)
	assert.True(t, claimed, "a lapsed claim must be taken over")
}

// A download can take far longer than the lease, so the instance doing it keeps
// renewing; only the absence of renewal -- a dead process -- lets it lapse.
func TestRenewClaims_KeepsALiveClaim(t *testing.T) {
	db := newTestDB(t)
	working, other := NewDownloadRepository(db, testLease), NewDownloadRepository(db, testLease)

	claimed, err := working.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	for range 4 {
		time.Sleep(testLease / 2)
		require.NoError(t, working.RenewClaims())
	}

	claimed, err = other.ClaimTransfer("100")
	require.NoError(t, err)
	assert.False(t, claimed, "a claim renewed past its original lease must still hold")
}

// Claims written before leases existed have none. Treating them as lapsed is
// what frees the transfers already stuck in deployed ledgers.
func TestClaimTransfer_ClaimWithoutALeaseIsTakenOver(t *testing.T) {
	db := newTestDB(t)

	_, err := db.Exec(`INSERT INTO downloads (transfer_id, downloaded_at, status, locked_by)
		VALUES ('100', '2026-01-01T00:00:00Z', 'downloading', 'a-process-long-gone')`)
	require.NoError(t, err)

	claimed, err := NewDownloadRepository(db, testLease).ClaimTransfer("100")
	require.NoError(t, err)
	assert.True(t, claimed)
}

// Every deployed ledger was created without a lease column. Opening one must
// add it rather than fail on the first claim.
func TestInitDB_UpgradesALedgerFromBeforeLeases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	old, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = old.Exec(`CREATE TABLE downloads (
		transfer_id TEXT UNIQUE,
		downloaded_at DATETIME,
		status TEXT DEFAULT 'pending',
		locked_by TEXT
	)`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	db, err := InitDB(context.Background(), path, 5, 2)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	claimed, err := NewDownloadRepository(db, testLease).ClaimTransfer("100")
	require.NoError(t, err)
	assert.True(t, claimed)

	// And opening it again is a no-op, not a duplicate-column error.
	again, err := InitDB(context.Background(), path, 5, 2)
	require.NoError(t, err)
	require.NoError(t, again.Close())
}

// An instance that stalls past its lease -- a paused container, a suspended
// host -- has had its transfer taken over by the time it resumes. Its outcome is
// stale: recording it would mark the transfer downloaded while the new holder is
// still writing it, or failed, releasing it to a third instance.
func TestUpdateTransferStatus_HolderThatLostItsClaimCannotRecordAnOutcome(t *testing.T) {
	db := newTestDB(t)
	stalled, takeover := NewDownloadRepository(db, testLease), NewDownloadRepository(db, testLease)

	claimed, err := stalled.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	time.Sleep(2 * testLease)

	claimed, err = takeover.ClaimTransfer("100")
	require.NoError(t, err)
	require.True(t, claimed)

	for _, outcome := range []string{storage.StatusDownloaded, "failed"} {
		err = stalled.UpdateTransferStatus("100", outcome)
		assert.ErrorIs(t, err, storage.ErrClaimLost, "a %s outcome from a lost claim must be refused", outcome)
	}

	require.NoError(t, takeover.UpdateTransferStatus("100", storage.StatusDownloaded),
		"the current holder records its outcome as usual")

	// After the download, the lifecycle is no longer anyone's claim: any instance
	// may move it on.
	require.NoError(t, stalled.UpdateTransferStatus("100", storage.StatusImported))
}
