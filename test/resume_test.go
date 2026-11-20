package test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// episode is content whose every position is distinguishable, so a resume that
// appended the wrong bytes -- a repeat, a gap, a restart glued onto the end --
// cannot produce an identical file by accident.
func episode(size int) string {
	var b strings.Builder

	for i := 0; b.Len() < size; i++ {
		b.WriteString(strings.Repeat(string(rune('a'+i%26)), 1+i%7))
	}

	return b.String()[:size]
}

// A 50 GB download was cut off after 42 GB and, unable to resume, could only
// start over from byte 0 -- so a file that took longer than whatever cut the
// connection could never complete. Each cut here must be continued from where
// it stopped, with a ranged request, into a byte-identical file.
func TestDownload_ResumesAfterTheConnectionIsCut(t *testing.T) {
	tests := []struct {
		name  string
		cuts  []int
		wants []int64
	}{
		{name: "cut once", cuts: []int{300}, wants: []int64{300}},
		{name: "cut twice", cuts: []int{300, 700}, wants: []int64{300, 700}},
		// More cuts than the budget for stalled attempts: each one got further,
		// so each one earns a fresh budget.
		{
			name:  "cut more often than the stall budget",
			cuts:  []int{100, 200, 300, 400, 500, 600, 700, 800},
			wants: []int64{100, 200, 300, 400, 500, 600, 700, 800},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := episode(1000)

			sb := seedbox.New(t, "itv", seedbox.Transfer{
				Name: "Long.Episode",
				Root: seedbox.Entry{Name: "Long.Episode", Children: []seedbox.Entry{
					{Name: "episode.mkv", Content: content, CutAt: tt.cuts},
				}},
			})

			dl, root := newDownloader(t, sb)

			transfers := fetch(t, sb)
			require.Len(t, transfers, 1)

			ctx := logctx.WithLogger(context.Background(), testLogger())

			count, err := dl.DownloadTransfer(ctx, transfers[0])
			require.NoError(t, err)
			assert.Equal(t, 1, count)

			assertFile(t, filepath.Join(root, "Long.Episode", "episode.mkv"), content)
			assert.Equal(t, tt.wants, sb.RangeStarts("episode.mkv"),
				"each resume must ask for the bytes from exactly where the last one stopped")
			assert.Equal(t, 1+len(tt.cuts), sb.Fetches("episode.mkv"))
		})
	}
}

// A server that ignores Range answers with the whole file. Appending that to
// the bytes already on disk would give the right size and the wrong content, so
// the download must start over instead -- and still end up correct.
func TestDownload_RestartsWhenRangeIsIgnored(t *testing.T) {
	content := episode(1000)

	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "No.Ranges",
		Root: seedbox.Entry{Name: "No.Ranges", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: content, CutAt: []int{600}, IgnoreRange: true},
		}},
	})

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	assertFile(t, filepath.Join(root, "No.Ranges", "episode.mkv"), content)

	// The cut fetch, the ranged one answered with the whole file, and the restart.
	assert.Equal(t, 3, sb.Fetches("episode.mkv"))
}

// A connection that is cut at the same place every time gets nowhere, so the
// resuming has to stop: a bounded number of attempts, then the failure the
// pipeline already handles.
func TestDownload_GivesUpWhenResumingGetsNowhere(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Always.Cut",
		Root: seedbox.Entry{Name: "Always.Cut", Children: []seedbox.Entry{
			{Name: "episode.mkv", Content: episode(100), AbortAfter: 40},
		}},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	count, err := dl.DownloadTransfer(ctx, transfers[0])
	require.Error(t, err)
	assert.Zero(t, count)

	// The first fetch, then five stalled resumes from the same offset.
	assert.Equal(t, 6, sb.Fetches("episode.mkv"))
	assert.Equal(t, []int64{40, 40, 40, 40, 40}, sb.RangeStarts("episode.mkv"))
}
