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
