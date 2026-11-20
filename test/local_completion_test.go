package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/http/rest"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/storage/sqlite"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allLocal is a ledger with every transfer on local disk, for tests about where
// a transfer is advertised rather than when it is complete.
type allLocal struct{}

func (allLocal) TransferStatuses(ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = storage.StatusDownloaded
	}

	return out, nil
}

// reported drives the real RPC handler, reading the real ledger, and returns the
// one torrent an *arr app would see.
func reported(t *testing.T, sb *seedbox.Seedbox, ledger rest.TransferLedger, localRoot string) rest.TransmissionTorrent {
	t.Helper()

	handler := rest.NewTransmissionHandler("u", "p", sb.Client(), ledger, sb.Label(), localRoot, nil)

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc",
		strings.NewReader(`{"method": "torrent-get", "arguments": {}}`))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp rest.TransmissionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "success", resp.Result)

	var args struct {
		Torrents []rest.TransmissionTorrent `json:"torrents"`
	}

	require.NoError(t, json.Unmarshal(resp.Arguments, &args))
	require.Len(t, args.Torrents, 1)

	return args.Torrents[0]
}

func newLedger(t *testing.T) *sqlite.DownloadRepository {
	t.Helper()

	db, err := sqlite.InitDB(context.Background(), filepath.Join(t.TempDir(), "downloads.db"), 5, 2)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return sqlite.NewDownloadRepository(db, time.Minute)
}

// The production failure, end to end: Put.io is seeding, the local download dies
// part way, and Radarr -- told the torrent was complete -- imported the partial
// file. The proxy must report it complete only once the pipeline has recorded it
// as written to disk.
func TestProxy_ReportsCompleteOnlyOnceTheTransferIsLocal(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name:   "Big.Movie.2026",
		Status: "SEEDING",
		Root: seedbox.Entry{
			Name: "Big.Movie.2026",
			Children: []seedbox.Entry{
				{Name: "movie.mkv", Content: "the whole fifty gigabytes", AbortAfter: 8},
			},
		},
	})

	dl, root := newDownloader(t, sb)
	ledger := newLedger(t)
	ctx := logctx.WithLogger(context.Background(), testLogger())

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	tr := transfers[0]

	// Put.io is done, but the pipeline has not recorded the transfer yet.
	before := reported(t, sb, ledger, root)
	assert.False(t, before.IsFinished, "not in the ledger yet, so not local")
	assert.Equal(t, rest.StatusDownload, before.Status)
	assert.Positive(t, before.LeftUntilDone)

	// Claimed and downloading.
	claimed, err := ledger.ClaimTransfer(tr.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	during := reported(t, sb, ledger, root)
	assert.False(t, during.IsFinished, "a local download in progress is not complete")
	assert.Equal(t, rest.StatusDownload, during.Status)
	assert.Positive(t, during.LeftUntilDone)

	// The local download dies part way, exactly as in production.
	_, err = dl.DownloadTransfer(ctx, tr)
	require.Error(t, err)
	require.NoError(t, ledger.UpdateTransferStatus(tr.ID, storage.StatusFailed))

	failed := reported(t, sb, ledger, root)
	assert.False(t, failed.IsFinished, "a failed local download must never look complete")
	assert.Equal(t, rest.StatusDownload, failed.Status)
	assert.Positive(t, failed.LeftUntilDone)
	require.NotNil(t, failed.ErrorString, "the *arr app is told why it is waiting")
}

func TestProxy_ReportsCompleteOnceDownloaded(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name:   "Small.Movie.2026",
		Status: "SEEDING",
		Root:   seedbox.Entry{Name: "Small.Movie.2026.mkv", Content: "every byte"},
	})

	dl, root := newDownloader(t, sb)
	ledger := newLedger(t)
	ctx := logctx.WithLogger(context.Background(), testLogger())

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	tr := transfers[0]

	claimed, err := ledger.ClaimTransfer(tr.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	_, err = dl.DownloadTransfer(ctx, tr)
	require.NoError(t, err)

	// Every byte is on disk, but until the pipeline records it, it is not done.
	written := reported(t, sb, ledger, root)
	assert.False(t, written.IsFinished)
	assert.Equal(t, int64(1), written.LeftUntilDone, "all bytes on disk, held at one until recorded")

	require.NoError(t, ledger.UpdateTransferStatus(tr.ID, storage.StatusDownloaded))

	done := reported(t, sb, ledger, root)
	assert.True(t, done.IsFinished)
	assert.Equal(t, rest.StatusSeed, done.Status)
	assert.Equal(t, int64(0), done.LeftUntilDone)
	assert.Nil(t, done.ErrorString)
}
