package test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDownloader wires the real downloader against a real seedbox client, writing
// into a temp directory. Nothing on the download path is mocked.
func newDownloader(t *testing.T, sb *seedbox.Seedbox) (*downloader.Downloader, string) {
	t.Helper()

	root := t.TempDir()
	client := sb.Client()

	return downloader.NewDownloader(root, 5, client, client, nil), root
}

// fetch resolves the seedbox's transfers the way production does, rather than
// hand-building a Transfer -- so the file paths under test are the ones the
// seedbox client actually derives.
func fetch(t *testing.T, sb *seedbox.Seedbox) []*transfer.Transfer {
	t.Helper()

	ctx := logctx.WithLogger(context.Background(), testLogger())

	transfers, err := sb.Client().GetTaggedTorrents(ctx, sb.Label())
	require.NoError(t, err)

	return transfers
}

func TestDownload_FolderTransferLandsOnDisk(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Show.S01.1080p-GROUP",
		Root: seedbox.Entry{
			Name: "Show.S01.1080p-GROUP",
			Children: []seedbox.Entry{
				{Name: "episode1.mkv", Content: "first episode bytes"},
				{Name: "episode2.mkv", Content: "second episode bytes"},
			},
		},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	tr := transfers[0]
	require.True(t, tr.IsDownloadable(), "a completed transfer should have discoverable files")

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, tr)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	assertFile(t, filepath.Join(root, "Show.S01.1080p-GROUP", "episode1.mkv"), "first episode bytes")
	assertFile(t, filepath.Join(root, "Show.S01.1080p-GROUP", "episode2.mkv"), "second episode bytes")
}

func TestDownload_NestedFoldersPreserveTheirStructure(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Show.Complete",
		Root: seedbox.Entry{
			Name: "Show.Complete",
			Children: []seedbox.Entry{
				{Name: "Season 1", Children: []seedbox.Entry{
					{Name: "s01e01.mkv", Content: "one one"},
				}},
				{Name: "Season 2", Children: []seedbox.Entry{
					{Name: "s02e01.mkv", Content: "two one"},
				}},
			},
		},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	assertFile(t, filepath.Join(root, "Show.Complete", "Season 1", "s01e01.mkv"), "one one")
	assertFile(t, filepath.Join(root, "Show.Complete", "Season 2", "s02e01.mkv"), "two one")
}

// Characterisation test, not an endorsement. A single-file transfer is currently
// written into a wrapper folder named after the file with its extension stripped,
// which is the divergence behind the import failures. Removing that wrapper is a
// separate change; when it lands this is the test that flips, which is exactly
// where the change should be visible.
func TestDownload_SingleFileTransferCurrentlyGetsAWrapperFolder(t *testing.T) {
	const stored = "Silo.S03E07.1080p-SiGLA ojqRfI77.mkv"

	sb := seedbox.New(t, "itv", seedbox.Transfer{
		// The transfer name and the stored file name deliberately differ, the way
		// they do when the seedbox appends a collision suffix.
		Name: "Silo.S03E07.1080p-SiGLA.mkv",
		Root: seedbox.Entry{Name: stored, Content: "episode bytes"},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	assertFile(t,
		filepath.Join(root, "Silo.S03E07.1080p-SiGLA ojqRfI77", stored),
		"episode bytes")

	// The wrapper is an invented directory: nothing advertises it, and the file the
	// seedbox actually stored is one level down from where a client would look.
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, entries[0].IsDir(), "today a single file arrives wrapped in a folder")
}

// The harness must be able to misbehave, or the tickets that depend on it cannot
// assert anything about failure. These two tests assert the fake's own injection
// points work; the downloader behaviour they drive belongs to #22.
func TestHarness_ServesAnErrorResponseForAFileFetch(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Broken",
		Root: seedbox.Entry{Name: "broken.mkv", Content: "never arrives", Status: 500},
	})

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)
	require.True(t, transfers[0].IsDownloadable())

	resp, err := http.Get(sb.DownloadURL(transfers[0].Files[0].ID))
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestHarness_ServesFewerBytesThanItReports(t *testing.T) {
	const full = "the full content"

	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Truncated",
		Root: seedbox.Entry{Name: "short.mkv", Content: full, TruncateTo: 4},
	})

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	file := transfers[0].Files[0]
	assert.EqualValues(t, len(full), file.Size,
		"the seedbox must report the full size, or a short read is not detectable at all")

	resp, err := http.Get(sb.DownloadURL(file.ID))
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)

	// The response is complete and well-formed -- it is simply shorter than the
	// reported size. Reading it produces no error, which is precisely why the
	// mismatch has to be caught by counting rather than by trusting the copy.
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, body, 4)
	assert.Less(t, int64(len(body)), file.Size)
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	require.NoError(t, err, "expected a file at %s", path)
	assert.Equal(t, want, string(got))
}
