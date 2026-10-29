package test

import (
	"context"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	seedTick = 10 * time.Millisecond

	seeding = "Seeding.Show.S01E01"
	// sixteen bytes, so uploading sixteen is a ratio of exactly 1.0.
	seedingContent = "sixteen bytes!!!"
)

func TestWatchForSeeding_RatioReachedFirst_CleansUp(t *testing.T) {
	sb, dl, tr := startSeeding(t)

	watch(t, dl, tr, downloader.SeedPolicy{Ratio: 1.0})

	sb.SetUploaded(seeding, int64(len(seedingContent)))

	assertCleanedUp(t, sb, dl)
}

// Most private trackers accept a minimum seeding time in place of a ratio, and an
// unpopular torrent may never reach the ratio at all. Without a time target the
// watch had no way to end and the transfer stayed on Put.io forever.
func TestWatchForSeeding_SeedTimeReachedFirst_CleansUp(t *testing.T) {
	sb, dl, tr := startSeeding(t)

	watch(t, dl, tr, downloader.SeedPolicy{Ratio: 1.0, Time: 72 * time.Hour})

	sb.SetSecondsSeeding(seeding, int64((72 * time.Hour).Seconds()))

	assertCleanedUp(t, sb, dl)
}

func TestWatchForSeeding_NeitherTargetReached_KeepsSeeding(t *testing.T) {
	sb, dl, tr := startSeeding(t)

	watch(t, dl, tr, downloader.SeedPolicy{Ratio: 1.0, Time: 72 * time.Hour})

	sb.SetUploaded(seeding, int64(len(seedingContent))/2)
	sb.SetSecondsSeeding(seeding, int64((71 * time.Hour).Seconds()))

	select {
	case <-dl.OnTransferCleanedUp:
		t.Fatal("a transfer short of both targets must not be cleaned up")
	case <-time.After(20 * seedTick):
	}

	assert.True(t, sb.HasTransfer(seeding))
}

// Once Put.io stops seeding a transfer, neither its ratio nor its seeding time
// grows again, so a target not yet reached never will be. Keeping it does nothing
// for the tracker and leaves it on Put.io forever, so it is cleaned up there and
// then.
func TestWatchForSeeding_SeedboxStopsSeeding_CleansUp(t *testing.T) {
	sb, dl, tr := startSeeding(t)

	watch(t, dl, tr, downloader.SeedPolicy{Ratio: 1.0, Time: 72 * time.Hour})

	sb.SetStatus(seeding, "COMPLETED")

	assertCleanedUp(t, sb, dl)
}

// startSeeding puts one seeding transfer on a fake seedbox, with nothing
// uploaded and no time spent seeding yet.
func startSeeding(t *testing.T) (*seedbox.Seedbox, *downloader.Downloader, *transfer.Transfer) {
	t.Helper()

	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name:   seeding,
		Status: "SEEDING",
		Root:   seedbox.Entry{Name: seeding + ".mkv", Content: seedingContent},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	return sb, dl, transfers[0]
}

func watch(t *testing.T, dl *downloader.Downloader, tr *transfer.Transfer, policy downloader.SeedPolicy) {
	t.Helper()

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), testLogger()))
	t.Cleanup(cancel)

	dl.WatchForSeeding(ctx, tr, seedTick, policy)
}

func assertCleanedUp(t *testing.T, sb *seedbox.Seedbox, dl *downloader.Downloader) {
	t.Helper()

	select {
	case <-dl.OnTransferCleanedUp:
	case <-time.After(wedgeTimeout):
		t.Fatal("the watch never reported the transfer cleaned up")
	}

	assert.False(t, sb.HasTransfer(seeding), "the transfer must be removed from Put.io")
	assert.False(t, sb.HasFile(seeding+".mkv"), "and its files with it")
}
