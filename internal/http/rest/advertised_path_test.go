package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// Each transfer is advertised under its own category, so Sonarr keeps only its own
// and Radarr keeps only its own. Both filter on exactly this: the label when the
// client reports label support, the directory otherwise -- and we now supply both.
func TestHandleTorrentGet_AdvertisesPerTransferCategories(t *testing.T) {
	const localRoot = "/data/Downloads/itv"

	got := torrentGet(t, localRoot,
		&transfer.Transfer{
			ID: "1", Status: "COMPLETED", Name: "Show.S01", Label: "tv",
			Files: files("Show.S01/ep.mkv"),
		},
		&transfer.Transfer{
			ID: "2", Status: "COMPLETED", Name: "Movie.2024", Label: "movies",
			Files: files("Movie.2024/movie.mkv"),
		},
	)

	require.Len(t, got, 2)

	assert.Equal(t, []string{"tv"}, got[0].Labels)
	assert.Equal(t, "/data/Downloads/itv/tv", got[0].DownloadDir)

	assert.Equal(t, []string{"movies"}, got[1].Labels)
	assert.Equal(t, "/data/Downloads/itv/movies", got[1].DownloadDir)
}

// A transfer with no category -- anything added before categories were honoured,
// or by an app with the field left blank -- stays where it was, advertised under
// the target label. An app with no category configured keeps everything, so these
// remain visible; an app with one configured will not see them, which is why the
// upgrade note says to let the queue drain before setting categories.
func TestHandleTorrentGet_UncategorisedTransfersKeepTheTargetLabel(t *testing.T) {
	got := torrentGet(t, "/data/Downloads/itv", &transfer.Transfer{
		ID: "1", Status: "COMPLETED", Name: "Legacy", Label: "",
		Files: files("Legacy/file.mkv"),
	})

	require.Len(t, got, 1)
	assert.Equal(t, []string{"itv"}, got[0].Labels)
	assert.Equal(t, "/data/Downloads/itv", got[0].DownloadDir, "no category means no subdirectory")
}

func TestTorrentAdd_PassesTheRequestedCategoryThrough(t *testing.T) {
	mockClient := &mockPutioClient{}
	handler := NewTransmissionHandler("u", "p", mockClient, "itv", "/data/Downloads/itv", nil)

	body := `{"method":"torrent-add","arguments":{"filename":"magnet:?xt=urn:btih:abc","labels":["tv"]}}`
	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(body))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "itv", mockClient.lastLabel, "the target label still scopes the account")
	assert.Equal(t, "tv", mockClient.lastCategory, "the requesting app's category picks the folder")
}

func TestTorrentAdd_NoCategoryFallsBackToTheTargetLabel(t *testing.T) {
	mockClient := &mockPutioClient{}
	handler := NewTransmissionHandler("u", "p", mockClient, "itv", "/data/Downloads/itv", nil)

	body := `{"method":"torrent-add","arguments":{"filename":"magnet:?xt=urn:btih:abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(body))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "itv", mockClient.lastLabel)
	assert.Empty(t, mockClient.lastCategory, "no category means the label's own folder, as before")
}

// The *arr apps check that "download directory plus category" exists and warn when
// it doesn't, so the directory is created as soon as an app first asks for that
// category rather than when the first download finishes.
func TestTorrentAdd_CreatesTheCategoryDirectory(t *testing.T) {
	root := t.TempDir()
	handler := NewTransmissionHandler("u", "p", &mockPutioClient{}, "itv", root, nil)

	body := `{"method":"torrent-add","arguments":{"filename":"magnet:?xt=urn:btih:abc","labels":["tv"]}}`
	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(body))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	info, err := os.Stat(filepath.Join(root, "tv"))
	require.NoError(t, err, "the category directory should exist immediately after the first add")
	assert.True(t, info.IsDir())
}

func TestTorrentAdd_NoCategoryCreatesNothing(t *testing.T) {
	root := t.TempDir()
	handler := NewTransmissionHandler("u", "p", &mockPutioClient{}, "itv", root, nil)

	body := `{"method":"torrent-add","arguments":{"filename":"magnet:?xt=urn:btih:abc"}}`
	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc", strings.NewReader(body))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
