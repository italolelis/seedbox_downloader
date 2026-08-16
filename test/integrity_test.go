package test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An error response must never become the file's content. Before this was
// checked, the copy succeeded and an HTML error page was written out under the
// media file's name and reported as a successful download.
func TestDownload_ErrorResponseIsNotContent(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Server.Error",
		Root: seedbox.Entry{Name: "Server.Error", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: "never reaches disk", Status: 500},
		}},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])

	require.Error(t, err, "a 500 response must fail the download")
	assert.Zero(t, count)

	_, statErr := os.Stat(filepath.Join(root, "Server.Error", "episode.mkv"))
	assert.True(t, os.IsNotExist(statErr), "no file should be left behind for a failed fetch")
}

// A file that no longer exists on the seedbox is a different condition from a
// broken one: the pipeline reports it as missing rather than retrying forever.
func TestDownload_MissingFileIsReportedAsGone(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Deleted",
		Root: seedbox.Entry{Name: "Deleted", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: "deleted from the account", Status: 404},
		}},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	_, err := dl.DownloadTransfer(ctx, transfers[0])

	require.Error(t, err)
	assert.ErrorIs(t, err, putio.ErrTransferFilesNotFound)
}

// The response here is complete and well-formed -- it is simply shorter than the
// size the seedbox reported. The copy returns no error at all, so counting bytes
// is the only thing that catches it.
func TestDownload_ShortBodyIsNotDownloaded(t *testing.T) {
	const full = "the complete episode content"

	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Truncated",
		Root: seedbox.Entry{Name: "Truncated", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: full, TruncateTo: 6},
		}},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])

	require.Error(t, err, "a body shorter than the reported size must fail the download")
	assert.True(t, errors.Is(err, downloader.ErrSizeMismatch),
		"the failure should identify itself as a size mismatch, not a generic error: %v", err)
	assert.Zero(t, count)

	// The short file is known-bad, so it must not remain on disk looking like media.
	_, statErr := os.Stat(filepath.Join(root, "Truncated", "episode.mkv"))
	assert.True(t, os.IsNotExist(statErr), "a file that failed its size check must be removed")
}

// A file whose size the seedbox reports correctly must still succeed -- the check
// has to be exact, not merely "close enough" or skipped.
func TestDownload_ExactSizeSucceeds(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Exact",
		Root: seedbox.Entry{Name: "Exact", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: "exactly these bytes"},
		}},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	assertFile(t, filepath.Join(root, "Exact", "episode.mkv"), "exactly these bytes")
}

// The fetch must honour the caller's context. Previously it ignored it entirely,
// so an in-flight download could not be cancelled at all.
func TestDownload_HonoursContextCancellation(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Cancelled",
		Root: seedbox.Entry{Name: "Cancelled", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: "content that will not be fetched"},
		}},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), testLogger()))
	cancel()

	done := make(chan error, 1)

	go func() {
		_, err := dl.DownloadTransfer(ctx, transfers[0])
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err, "a cancelled context must abort the download")
	case <-time.After(wedgeTimeout):
		t.Fatal("download ignored a cancelled context")
	}
}
