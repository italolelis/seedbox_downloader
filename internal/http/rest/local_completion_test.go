package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allLocal is a ledger that has every transfer on local disk, for tests about
// something other than when a transfer is reported complete.
type allLocal struct{}

func (allLocal) TransferStatuses(ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = storage.StatusDownloaded
	}

	return out, nil
}

// ledgerStub answers from a fixed map, or fails with err.
type ledgerStub struct {
	statuses map[string]string
	err      error
}

func (l ledgerStub) TransferStatuses(ids []string) (map[string]string, error) {
	if l.err != nil {
		return nil, l.err
	}

	out := map[string]string{}

	for _, id := range ids {
		if s, ok := l.statuses[id]; ok {
			out[id] = s
		}
	}

	return out, nil
}

// rpcTorrentGet runs torrent-get against ledger and returns the RPC result and
// the torrents an *arr app would see.
func rpcTorrentGet(
	t *testing.T, ledger TransferLedger, localRoot string, transfers ...*transfer.Transfer,
) (string, []TransmissionTorrent) {
	t.Helper()

	mockClient := &mockPutioClient{
		getTaggedTorrentsFunc: func(_ context.Context, _ string) ([]*transfer.Transfer, error) {
			return transfers, nil
		},
	}

	handler := NewTransmissionHandler("u", "p", mockClient, ledger, "itv", localRoot, nil)

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc",
		strings.NewReader(`{"method": "torrent-get", "arguments": {}}`))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp TransmissionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	if resp.Result != "success" {
		return resp.Result, nil
	}

	var args struct {
		Torrents []TransmissionTorrent `json:"torrents"`
	}

	require.NoError(t, json.Unmarshal(resp.Arguments, &args))

	return resp.Result, args.Torrents
}

// seededOnPutio is a transfer Put.io has finished: every byte downloaded there.
func seededOnPutio(status string) *transfer.Transfer {
	return &transfer.Transfer{
		ID:         "42",
		Name:       "Movie.2026",
		Status:     status,
		Size:       1000,
		Downloaded: 1000,
		Files:      []*transfer.File{{ID: 1, Path: "Movie.2026.mkv", Size: 1000}},
	}
}

// Radarr imported a 41.6 GB partial of a 50 GB file because the proxy reported
// the torrent complete on Put.io's word alone, while the local copy had just
// failed. Until the ledger says the transfer is on local disk, it must look like
// a download in progress, whatever Put.io says.
func TestHandleTorrentGet_SeedboxCompleteButNotLocal_IsStillDownloading(t *testing.T) {
	tests := []struct {
		name         string
		putioStatus  string
		ledgerStatus string // "" means the ledger has no row
		wantWarning  bool
	}{
		{"not yet in the ledger", "SEEDING", "", false},
		{"claimed, not started", "COMPLETED", "pending", false},
		{"local download in progress", "SEEDING", "downloading", false},
		{"local download in progress, seedbox finished", "FINISHED", "downloading", false},
		{"local download in progress, seedbox seedingwait", "SEEDINGWAIT", "downloading", false},
		{"local download failed", "SEEDING", storage.StatusFailed, true},
		{"went missing mid download", "COMPLETED", storage.StatusMissing, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ledger := ledgerStub{statuses: map[string]string{}}
			if tt.ledgerStatus != "" {
				ledger.statuses["42"] = tt.ledgerStatus
			}

			result, torrents := rpcTorrentGet(t, ledger, t.TempDir(), seededOnPutio(tt.putioStatus))
			require.Equal(t, "success", result)
			require.Len(t, torrents, 1)

			got := torrents[0]
			assert.Equal(t, StatusDownload, got.Status, "must be reported as downloading")
			assert.False(t, got.IsFinished, "must not be reported finished")
			assert.Equal(t, int64(1000), got.LeftUntilDone, "nothing is on disk, so all of it is left")

			if tt.wantWarning {
				require.NotNil(t, got.ErrorString, "a hold that will not clear by waiting says why")
				assert.NotEmpty(t, *got.ErrorString)
			} else {
				assert.Nil(t, got.ErrorString)
			}
		})
	}
}

// Once the downloader has recorded the transfer as written to local disk -- and
// at every later step -- the seedbox's own state is reported, so imports proceed.
func TestHandleTorrentGet_LocalDownloadDone_IsComplete(t *testing.T) {
	for _, status := range []string{storage.StatusDownloaded, storage.StatusImported, storage.StatusCleaned} {
		t.Run(status, func(t *testing.T) {
			ledger := ledgerStub{statuses: map[string]string{"42": status}}

			result, torrents := rpcTorrentGet(t, ledger, t.TempDir(), seededOnPutio("SEEDING"))
			require.Equal(t, "success", result)
			require.Len(t, torrents, 1)

			got := torrents[0]
			assert.Equal(t, StatusSeed, got.Status)
			assert.True(t, got.IsFinished)
			assert.Equal(t, int64(0), got.LeftUntilDone)
			assert.Nil(t, got.ErrorString)
		})
	}
}

// While Put.io is still downloading, the ledger has nothing to add: the transfer
// keeps Put.io's own progress.
func TestHandleTorrentGet_SeedboxStillDownloading_KeepsSeedboxProgress(t *testing.T) {
	tr := &transfer.Transfer{ID: "7", Name: "Show.S01E01", Status: "DOWNLOADING", Size: 1000, Downloaded: 250}

	result, torrents := rpcTorrentGet(t, ledgerStub{}, t.TempDir(), tr)
	require.Equal(t, "success", result)
	require.Len(t, torrents, 1)

	assert.Equal(t, StatusDownload, torrents[0].Status)
	assert.Equal(t, int64(750), torrents[0].LeftUntilDone)
	assert.False(t, torrents[0].IsFinished)
}

// The progress shown while held is what is actually on disk, and never reaches
// zero left, since zero left is itself read as complete.
func TestHandleTorrentGet_HeldTransferReportsLocalProgress(t *testing.T) {
	root := t.TempDir()
	tr := seededOnPutio("SEEDING")

	require.NoError(t, os.WriteFile(filepath.Join(root, "Movie.2026.mkv"), make([]byte, 400), 0o600))

	_, torrents := rpcTorrentGet(t, ledgerStub{statuses: map[string]string{"42": "downloading"}}, root, tr)
	require.Len(t, torrents, 1)
	assert.Equal(t, int64(600), torrents[0].LeftUntilDone)

	// Every byte written, but not yet recorded as done: still not complete.
	require.NoError(t, os.WriteFile(filepath.Join(root, "Movie.2026.mkv"), make([]byte, 1000), 0o600))

	_, torrents = rpcTorrentGet(t, ledgerStub{statuses: map[string]string{"42": "downloading"}}, root, tr)
	require.Len(t, torrents, 1)
	assert.Equal(t, int64(1), torrents[0].LeftUntilDone)
	assert.Equal(t, StatusDownload, torrents[0].Status)
}

// A transfer the pipeline never downloads, because the seedbox lists no files for
// it, must not sit as a silent download forever: it carries a reason.
func TestHandleTorrentGet_NothingToDownload_SaysWhy(t *testing.T) {
	tr := seededOnPutio("COMPLETED")
	tr.Files = nil

	_, torrents := rpcTorrentGet(t, ledgerStub{}, t.TempDir(), tr)
	require.Len(t, torrents, 1)

	assert.False(t, torrents[0].IsFinished)
	require.NotNil(t, torrents[0].ErrorString)
}

// Without a ledger to ask, nothing is known to be local, so nothing is complete.
func TestHandleTorrentGet_NoLedger_NeverComplete(t *testing.T) {
	_, torrents := rpcTorrentGet(t, nil, t.TempDir(), seededOnPutio("SEEDING"))
	require.Len(t, torrents, 1)

	assert.False(t, torrents[0].IsFinished)
	assert.Equal(t, StatusDownload, torrents[0].Status)
}

// If the ledger cannot be read the call fails, rather than guessing.
func TestHandleTorrentGet_LedgerError_FailsTheCall(t *testing.T) {
	result, _ := rpcTorrentGet(t, ledgerStub{err: errors.New("database is locked")}, t.TempDir(), seededOnPutio("SEEDING"))

	assert.NotEqual(t, "success", result)
	assert.Contains(t, result, "local download state")
}
