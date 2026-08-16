package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// torrentGet drives the real handler over HTTP and returns what it advertised.
func torrentGet(t *testing.T, localRoot string, transfers ...*transfer.Transfer) []TransmissionTorrent {
	t.Helper()

	mockClient := &mockPutioClient{
		getTaggedTorrentsFunc: func(_ context.Context, _ string) ([]*transfer.Transfer, error) {
			return transfers, nil
		},
	}

	handler := NewTransmissionHandler("testuser", "testpass", mockClient, "itv", localRoot, nil)

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc",
		strings.NewReader(`{"method": "torrent-get", "arguments": {}}`))
	req.SetBasicAuth("testuser", "testpass")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp TransmissionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var args struct {
		Torrents []TransmissionTorrent `json:"torrents"`
	}

	require.NoError(t, json.Unmarshal(resp.Arguments, &args))

	return args.Torrents
}

func files(paths ...string) []*transfer.File {
	out := make([]*transfer.File, 0, len(paths))
	for i, p := range paths {
		out = append(out, &transfer.File{ID: int64(i + 1), Path: p, Size: 1})
	}

	return out
}

// The four transfers reported in #9. Every one must advertise a name taken from
// the files rather than from the transfer, and the local root as the directory.
func TestHandleTorrentGet_AdvertisesTheLocalLayout(t *testing.T) {
	const localRoot = "/data/Downloads/itv"

	tests := []struct {
		name     string
		transfer *transfer.Transfer
		wantName string
	}{
		{
			name: "folder transfer",
			transfer: &transfer.Transfer{
				ID: "1", Status: "COMPLETED", Name: "Jerry.and.Marge.Go.Large.2022-FGT",
				RemoteFolder: "/itv",
				Files: files(
					"Jerry.and.Marge.Go.Large.2022-FGT/movie.mkv",
					"Jerry.and.Marge.Go.Large.2022-FGT/sample.mkv",
				),
			},
			wantName: "Jerry.and.Marge.Go.Large.2022-FGT",
		},
		{
			name: "single file diverging by its extension",
			transfer: &transfer.Transfer{
				ID: "2", Status: "COMPLETED", Name: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
				RemoteFolder: "/itv",
				Files:        files("Silo.S03E07.1080p.DUAL-SiGLA.mkv"),
			},
			wantName: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
		},
		{
			name: "single file carrying a collision suffix",
			transfer: &transfer.Transfer{
				ID: "3", Status: "COMPLETED", Name: "Silo S03E08 HiggsBoson .exe",
				RemoteFolder: "/itv",
				Files:        files("Silo S03E08 HiggsBoson  ojqRfI77.exe"),
			},
			wantName: "Silo S03E08 HiggsBoson  ojqRfI77.exe",
		},
		{
			name: "single file renamed entirely by the seedbox",
			transfer: &transfer.Transfer{
				ID: "4", Status: "COMPLETED", Name: "Minions.2015.2160p.H265.MP4-BTM",
				RemoteFolder: "/itv",
				Files:        files("Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4"),
			},
			wantName: "Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := torrentGet(t, localRoot, tt.transfer)

			require.Len(t, got, 1)
			assert.Equal(t, tt.wantName, got[0].Name,
				"the advertised name must come from the files, not the transfer name")
			assert.Equal(t, localRoot, got[0].DownloadDir,
				"the advertised directory must be the local root")
		})
	}
}

// The seedbox-side folder must not leak into the advertisement. It used to be sent
// as the download directory, which is why imports only worked when a remote path
// mapping happened to compensate for it.
func TestHandleTorrentGet_DoesNotAdvertiseTheRemoteFolder(t *testing.T) {
	got := torrentGet(t, "/data/Downloads/itv", &transfer.Transfer{
		ID: "1", Status: "COMPLETED", Name: "Show.S01",
		RemoteFolder: "/itv",
		Files:        files("Show.S01/episode.mkv"),
	})

	require.Len(t, got, 1)
	assert.NotEqual(t, "/itv", got[0].DownloadDir)
	assert.Equal(t, "/data/Downloads/itv", got[0].DownloadDir)
}

// An in-progress transfer has no discoverable files yet, so there is nothing to
// derive a name from. It still has to appear in the queue with something sensible.
func TestHandleTorrentGet_InProgressTransferFallsBackToTheTransferName(t *testing.T) {
	got := torrentGet(t, "/data/Downloads/itv", &transfer.Transfer{
		ID: "1", Status: "DOWNLOADING", Name: "Still.Downloading-GROUP",
		RemoteFolder: "/itv",
		Files:        nil,
	})

	require.Len(t, got, 1)
	assert.Equal(t, "Still.Downloading-GROUP", got[0].Name)
	assert.Equal(t, "/data/Downloads/itv", got[0].DownloadDir)
}

// session-get reports the client's default download directory, which the *arr apps
// read when configuring themselves. It must be the local root for the same reason.
func TestSessionGet_ReportsTheLocalRoot(t *testing.T) {
	const localRoot = "/data/Downloads/itv"

	handler := NewTransmissionHandler("testuser", "testpass", &mockPutioClient{}, "itv", localRoot, nil)

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc",
		strings.NewReader(`{"method": "session-get", "arguments": {}}`))
	req.SetBasicAuth("testuser", "testpass")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp TransmissionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var cfg TransmissionConfig
	require.NoError(t, json.Unmarshal(resp.Arguments, &cfg))

	assert.Equal(t, localRoot, cfg.DownloadDir)
}
