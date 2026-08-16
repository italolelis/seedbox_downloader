package test

import (
	"context"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wedgeTimeout is long enough that a slow machine won't trip it, and short
// enough that a wedge fails the run rather than hanging it.
const wedgeTimeout = 5 * time.Second

// A file whose transfer fails mid-stream used to block forever on a per-file
// event channel that nothing received from, so this call never returned. The
// timeout is the entire point of the test: without it, a regression hangs the
// suite instead of failing it.
func TestDownloadTransfer_ReturnsWhenAFileFails(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "Aborts.Midway",
		Root: seedbox.Entry{
			Name: "Aborts.Midway",
			Children: []seedbox.Entry{
				{Name: "episode.mkv", Content: "the full episode content", AbortAfter: 4},
			},
		},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx := logctx.WithLogger(context.Background(), testLogger())

	done := make(chan error, 1)

	go func() {
		_, err := dl.DownloadTransfer(ctx, transfers[0])
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err, "a mid-stream failure must surface as an error")
	case <-time.After(wedgeTimeout):
		t.Fatal("DownloadTransfer blocked instead of returning: the pipeline is wedged")
	}
}

// The failure of one transfer must not stop the next one. This drives the real
// watch loop, which is where the wedge actually stalled the pipeline: it consumes
// transfers one at a time, so a call that never returns starves everything after.
func TestWatchDownloads_KeepsGoingAfterATransferFails(t *testing.T) {
	sb := seedbox.New(t,
		"itv",
		seedbox.Transfer{
			Name: "Broken",
			Root: seedbox.Entry{Name: "Broken", Children: []seedbox.Entry{
				{Name: "broken.mkv", Content: "content that never arrives", AbortAfter: 4},
			}},
		},
		seedbox.Transfer{
			Name: "Healthy",
			Root: seedbox.Entry{Name: "Healthy", Children: []seedbox.Entry{
				{Name: "good.mkv", Content: "content that arrives fine"},
			}},
		},
	)

	dl, root := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 2)

	broken, healthy := byName(t, transfers, "Broken"), byName(t, transfers, "Healthy")

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), testLogger()))
	defer cancel()

	queue := make(chan *transfer.Transfer)
	dl.WatchDownloads(ctx, queue)

	failed := collect(dl.OnTransferDownloadError)
	finished := collect(dl.OnTransferDownloadFinished)

	queue <- broken

	select {
	case got := <-failed:
		assert.Equal(t, broken.ID, got.ID)
	case <-time.After(wedgeTimeout):
		t.Fatal("the failing transfer never reported an error: the pipeline is wedged")
	}

	// The real assertion: the loop is still alive and serving the queue.
	queue <- healthy

	select {
	case got := <-finished:
		assert.Equal(t, healthy.ID, got.ID)
	case <-time.After(wedgeTimeout):
		t.Fatal("a healthy transfer queued after a failure was never downloaded")
	}

	assertFile(t, root+"/Healthy/good.mkv", "content that arrives fine")
}

// Nothing closes the event channels any more, so cancelling mid-download cannot
// panic with a send on a closed channel. Cancellation while a download is in
// flight is the window in which that used to be possible.
func TestShutdownMidDownload_DoesNotPanic(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name: "InFlight",
		Root: seedbox.Entry{Name: "InFlight", Children: []seedbox.Entry{
			{Name: "a.mkv", Content: "some bytes"},
			{Name: "b.mkv", Content: "more bytes", AbortAfter: 2},
		}},
	})

	dl, _ := newDownloader(t, sb)

	transfers := fetch(t, sb)
	require.Len(t, transfers, 1)

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), testLogger()))

	queue := make(chan *transfer.Transfer)
	dl.WatchDownloads(ctx, queue)

	// Drain, so the watch loop is never itself the thing that blocks.
	failed := collect(dl.OnTransferDownloadError)
	finished := collect(dl.OnTransferDownloadFinished)

	queue <- transfers[0]

	// Cancel while the transfer is being worked on, then give the goroutines a
	// window to unwind. A panic in any of them fails the test process.
	cancel()
	time.Sleep(200 * time.Millisecond)

	select {
	case <-failed:
	case <-finished:
	default:
	}
}

// collect drains a channel into a buffered one so a test can wait on it without
// the producer blocking on an unbuffered send.
func collect[T any](in <-chan T) <-chan T {
	out := make(chan T, 8)

	go func() {
		for v := range in {
			out <- v
		}
	}()

	return out
}

func byName(t *testing.T, transfers []*transfer.Transfer, name string) *transfer.Transfer {
	t.Helper()

	for _, tr := range transfers {
		if tr.Name == name {
			return tr
		}
	}

	t.Fatalf("no transfer named %q", name)

	return nil
}
