package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/http/rest"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// advertised drives the real RPC handler against the real seedbox client and
// returns what an *arr app would receive.
func advertised(t *testing.T, sb *seedbox.Seedbox, localRoot string) []rest.TransmissionTorrent {
	t.Helper()

	handler := rest.NewTransmissionHandler("u", "p", sb.Client(), sb.Label(), localRoot, nil)

	req := httptest.NewRequest(http.MethodPost, "/transmission/rpc",
		strings.NewReader(`{"method": "torrent-get", "arguments": {}}`))
	req.SetBasicAuth("u", "p")

	w := httptest.NewRecorder()
	handler.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp rest.TransmissionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var args struct {
		Torrents []rest.TransmissionTorrent `json:"torrents"`
	}

	require.NoError(t, json.Unmarshal(resp.Arguments, &args))

	return args.Torrents
}

// This is the invariant behind #9, stated as directly as it can be: whatever path
// we advertise over RPC must be a path that exists on disk. Sonarr and Radarr both
// compute the import path as the advertised directory joined with the advertised
// name, with no branching on single- versus multi-file, then look for a directory
// and fall back to a file. If the join does not resolve, nothing imports -- which
// is exactly what was happening.
//
// No single-module test can express this: the path is written by the downloader and
// advertised by the RPC adapter. This runs both, for real, against one seedbox.
func TestInvariant_AdvertisedPathExistsOnDisk(t *testing.T) {
	tests := []struct {
		name         string
		transfer     seedbox.Transfer
		wantIsDir    bool
		wantAdvertis string
	}{
		{
			name: "folder transfer",
			transfer: seedbox.Transfer{
				Name: "Jerry.and.Marge.Go.Large.2022-FGT",
				Root: seedbox.Entry{Name: "Jerry.and.Marge.Go.Large.2022-FGT", Children: []seedbox.Entry{
					{Name: "movie.mkv", Content: "movie bytes"},
					{Name: "sample.mkv", Content: "sample bytes"},
				}},
			},
			wantIsDir:    true,
			wantAdvertis: "Jerry.and.Marge.Go.Large.2022-FGT",
		},
		{
			name: "single file diverging by its extension",
			transfer: seedbox.Transfer{
				Name: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
				Root: seedbox.Entry{Name: "Silo.S03E07.1080p.DUAL-SiGLA.mkv", Content: "episode bytes"},
			},
			wantIsDir:    false,
			wantAdvertis: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
		},
		{
			name: "single file carrying a collision suffix",
			transfer: seedbox.Transfer{
				Name: "Silo S03E08 HiggsBoson .exe",
				Root: seedbox.Entry{Name: "Silo S03E08 HiggsBoson  ojqRfI77.exe", Content: "suffixed bytes"},
			},
			wantIsDir:    false,
			wantAdvertis: "Silo S03E08 HiggsBoson  ojqRfI77.exe",
		},
		{
			name: "single file renamed entirely by the seedbox",
			transfer: seedbox.Transfer{
				Name: "Minions.2015.2160p.H265.MP4-BTM",
				Root: seedbox.Entry{Name: "Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4", Content: "renamed bytes"},
			},
			wantIsDir:    false,
			wantAdvertis: "Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4",
		},
		{
			name: "nested folder transfer",
			transfer: seedbox.Transfer{
				Name: "Show.Complete",
				Root: seedbox.Entry{Name: "Show.Complete", Children: []seedbox.Entry{
					{Name: "Season 1", Children: []seedbox.Entry{
						{Name: "s01e01.mkv", Content: "nested bytes"},
					}},
				}},
			},
			wantIsDir:    true,
			wantAdvertis: "Show.Complete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := seedbox.New(t, "itv", tt.transfer)
			dl, root := newDownloader(t, sb)

			transfers := fetch(t, sb)
			require.Len(t, transfers, 1)

			ctx := logctx.WithLogger(context.Background(), testLogger())

			_, err := dl.DownloadTransfer(ctx, transfers[0])
			require.NoError(t, err)

			// Now ask the RPC what it would tell Sonarr/Radarr.
			torrents := advertised(t, sb, root)
			require.Len(t, torrents, 1)

			assert.Equal(t, tt.wantAdvertis, torrents[0].Name)
			assert.Equal(t, root, torrents[0].DownloadDir)

			// The assertion that matters.
			importPath := filepath.Join(torrents[0].DownloadDir, torrents[0].Name)

			info, statErr := os.Stat(importPath)
			require.NoError(t, statErr,
				"the advertised path does not exist, so nothing would ever import: %s", importPath)
			assert.Equal(t, tt.wantIsDir, info.IsDir(),
				"the advertised path resolved to the wrong kind of entry")
		})
	}
}

// All four reported transfers on one account at once, which is how they actually
// occurred -- a shared seedbox where several land in the same label folder.
func TestInvariant_AllReportedCasesResolveTogether(t *testing.T) {
	sb := seedbox.New(t, "itv",
		seedbox.Transfer{
			Name: "Jerry.and.Marge.Go.Large.2022-FGT",
			Root: seedbox.Entry{Name: "Jerry.and.Marge.Go.Large.2022-FGT", Children: []seedbox.Entry{
				{Name: "movie.mkv", Content: "movie bytes"},
			}},
		},
		seedbox.Transfer{
			Name: "Silo.S03E07.1080p.DUAL-SiGLA.mkv",
			Root: seedbox.Entry{Name: "Silo.S03E07.1080p.DUAL-SiGLA.mkv", Content: "episode bytes"},
		},
		seedbox.Transfer{
			Name: "Silo S03E08 HiggsBoson .exe",
			Root: seedbox.Entry{Name: "Silo S03E08 HiggsBoson  ojqRfI77.exe", Content: "suffixed bytes"},
		},
		seedbox.Transfer{
			Name: "Minions.2015.2160p.H265.MP4-BTM",
			Root: seedbox.Entry{Name: "Minions.2015.2160p.HDR10+[Ben The Men] ojqYDnu2.mp4", Content: "renamed bytes"},
		},
	)

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 4)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	for _, tr := range transfers {
		_, err := dl.DownloadTransfer(ctx, tr)
		require.NoError(t, err, "downloading %q", tr.Name)
	}

	torrents := advertised(t, sb, root)
	require.Len(t, torrents, 4)

	for _, tor := range torrents {
		importPath := filepath.Join(tor.DownloadDir, tor.Name)

		_, statErr := os.Stat(importPath)
		assert.NoError(t, statErr, "advertised path missing for %q: %s", tor.Name, importPath)
	}
}

// An in-progress transfer has no files yet, so there is nothing on disk and nothing
// to derive from. It must still be advertised, so the *arr app can show it as
// downloading rather than losing track of it.
func TestInvariant_InProgressTransferIsStillAdvertised(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name:       "Still.Downloading-GROUP",
		Root:       seedbox.Entry{Name: "Still.Downloading-GROUP", Content: "not yet"},
		Status:     "DOWNLOADING",
		InProgress: true,
	})

	torrents := advertised(t, sb, t.TempDir())
	require.Len(t, torrents, 1)

	assert.Equal(t, "Still.Downloading-GROUP", torrents[0].Name,
		"an in-progress transfer falls back to the transfer name")
}
