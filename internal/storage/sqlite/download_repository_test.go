package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRepo(t *testing.T) *DownloadRepository {
	t.Helper()

	db, err := InitDB(context.Background(), filepath.Join(t.TempDir(), "test.db"), 5, 2)
	require.NoError(t, err)

	t.Cleanup(func() { db.Close() })

	return NewDownloadRepository(db)
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
