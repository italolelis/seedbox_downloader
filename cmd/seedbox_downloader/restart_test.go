package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/storage/sqlite"
	"github.com/italolelis/seedbox_downloader/internal/svc/arr"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// tick is the polling interval for every loop in these tests, so a whole
	// download-import-seed cycle takes milliseconds rather than minutes.
	tick = 20 * time.Millisecond

	// lease is the claim lease in these tests: long enough to span several
	// renewals, short enough to let one lapse within a test.
	lease = 10 * tick

	// settle bounds every wait. Long enough for a slow CI machine, short enough
	// that a lost watch fails the run instead of hanging it.
	settle = 5 * time.Second

	// The fake's one transfer: a folder, so its local output is easy to find.
	transferName = "Show.S01E01.1080p-GROUP"
	content      = "sixteen bytes!!!"
)

// A restart while a transfer is seeding used to drop its watch on the floor: the
// ledger said "downloaded", the orchestrator skipped it, and nothing ever looked
// at its ratio again -- so it stayed on Put.io forever.
func TestRestart_MidSeed_TransferIsStillCleanedUp(t *testing.T) {
	sb := newSeedbox(t)
	arrApp := newFakeArr(t)
	arrApp.imported.Store(true)

	h := newHarness(t, sb, arrApp, 1.0)

	first := h.start()
	h.waitForStatus("imported")
	first() // the restart

	require.True(t, sb.HasTransfer(transferName), "the ratio is not reached yet, so the transfer must still be seeding")

	h.start()
	sb.SetUploaded(transferName, int64(len(content)))

	h.waitForCleanup(sb)
}

// The same loss one step earlier: a restart between download and import dropped
// the import watch, so the import was never noticed, the local copy was never
// removed, and the transfer was never taken off Put.io.
func TestRestart_BeforeImport_TransferIsStillCleanedUp(t *testing.T) {
	sb := newSeedbox(t)
	arrApp := newFakeArr(t)

	h := newHarness(t, sb, arrApp, 0)

	first := h.start()
	h.waitForStatus(storage.StatusDownloaded)
	first()

	local := filepath.Join(h.cfg.DownloadDir, transferName)
	require.DirExists(t, local, "the transfer was downloaded and is waiting to be imported")

	h.start()
	arrApp.imported.Store(true)

	h.waitForCleanup(sb)
	require.NoDirExists(t, local, "once imported, the local copy is removed")
}

// A transfer missing from one listing is not gone from the seedbox: the listing
// drops anything whose folder lookup fails, so a Put.io hiccup at startup used to
// close out a transfer that was still seeding -- leaving it there forever.
func TestRestart_SeedboxHiccupAtStartup_TransferIsStillCleanedUp(t *testing.T) {
	sb := newSeedbox(t)
	arrApp := newFakeArr(t)
	arrApp.imported.Store(true)

	h := newHarness(t, sb, arrApp, 1.0)

	first := h.start()
	h.waitForStatus(storage.StatusImported)
	first()

	sb.FailFileGets(true)
	h.start()
	time.Sleep(5 * tick) // let the resume run against the failing seedbox

	sb.FailFileGets(false)
	sb.SetUploaded(transferName, int64(len(content)))

	h.waitForCleanup(sb)
}

// The same hiccup before import, where the listing is still needed for the
// transfer's files: the resume waits for the seedbox to list it again rather
// than closing it out.
func TestRestart_SeedboxHiccupBeforeImport_TransferIsStillCleanedUp(t *testing.T) {
	sb := newSeedbox(t)
	arrApp := newFakeArr(t)

	h := newHarness(t, sb, arrApp, 0)

	first := h.start()
	h.waitForStatus(storage.StatusDownloaded)
	first()

	sb.FailFileGets(true)
	h.start()
	time.Sleep(5 * tick)

	sb.FailFileGets(false)
	arrApp.imported.Store(true)

	h.waitForCleanup(sb)
}

// A transfer the ledger still calls downloaded but the seedbox no longer has
// leaves nothing on Put.io to clean up. Every transfer that finished before the
// ledger recorded cleanup looks like this, so on the first start after upgrading
// they are closed out rather than each getting an import watch that may never end.
func TestResume_TransferGoneFromTheSeedboxIsClosedOut(t *testing.T) {
	sb := newSeedbox(t)
	h := newHarness(t, sb, newFakeArr(t), 0)

	const goneID = "999"

	_, err := h.repo.ClaimTransfer(goneID)
	require.NoError(t, err)
	require.NoError(t, h.repo.UpdateTransferStatus(goneID, storage.StatusDownloaded))

	h.start()

	h.waitForStatusOf(goneID, storage.StatusCleaned)
}

// A process that dies mid-download leaves its claim behind and never marks the
// transfer failed. That claim used to hold forever, so the transfer was never
// downloaded by anyone. Once the lease lapses, the next instance takes it over.
func TestCrashMidDownload_TransferIsTakenOver(t *testing.T) {
	sb := newSeedbox(t)
	h := newHarness(t, sb, newFakeArr(t), 0)

	// The claim of a process that died: made, and never renewed.
	claimed, err := sqlite.NewDownloadRepository(h.db, lease).ClaimTransfer(fakeTransferID)
	require.NoError(t, err)
	require.True(t, claimed)

	h.start()

	h.waitForStatus(storage.StatusDownloaded)
	assert.FileExists(t, filepath.Join(h.cfg.DownloadDir, transferName, "episode.mkv"))
}

// The other side of the lease: a download that takes many leases' worth of time
// must not be taken over by a second instance while the first is alive and
// renewing, or the same transfer is fetched twice.
func TestTwoInstances_LongDownloadIsNotTakenOver(t *testing.T) {
	sb := seedbox.New(t, "itv", seedbox.Transfer{
		Name:   transferName,
		Status: "SEEDING",
		Root: seedbox.Entry{
			Name:     transferName,
			Children: []seedbox.Entry{{Name: "episode.mkv", Content: content, Delay: 4 * lease}},
		},
	})
	h := newHarness(t, sb, newFakeArr(t), 0)

	h.start()
	require.Eventually(t, func() bool { return sb.Fetches("episode.mkv") == 1 }, settle, tick)

	h.start() // a second instance sharing the ledger and the seedbox account

	h.waitForStatus(storage.StatusDownloaded)
	assert.Equal(t, 1, sb.Fetches("episode.mkv"), "a live claim was taken over and the file fetched twice")
}

// harness runs the real pipeline against a fake seedbox and a fake *arr app,
// with a ledger on disk that outlives each run -- which is what lets a test
// restart it.
type harness struct {
	t   *testing.T
	cfg *config
	db  *sql.DB
	// repo reads the ledger for assertions. Each started pipeline gets its own,
	// because each process is its own instance.
	repo *sqlite.DownloadRepository
	sb   *seedbox.Seedbox
	arr  []*arr.Client
}

func newHarness(t *testing.T, sb *seedbox.Seedbox, arrApp *fakeArr, seedRatio float64) *harness {
	t.Helper()

	db, err := sqlite.InitDB(context.Background(), filepath.Join(t.TempDir(), "downloads.db"), 5, 2)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return &harness{
		t: t,
		cfg: &config{
			DownloadDir:     t.TempDir(),
			MaxParallel:     2,
			TargetLabel:     sb.Label(),
			PollingInterval: tick,
			PutioSeedRatio:  seedRatio,
			ClaimLease:      lease,
		},
		db:   db,
		repo: sqlite.NewDownloadRepository(db, lease),
		sb:   sb,
		arr:  []*arr.Client{arr.NewClient("test-key", arrApp.server.URL)},
	}
}

// start runs one process's worth of pipeline and returns the function that
// stops it. Stopping is a cancelled context, exactly as on SIGTERM.
func (h *harness) start() func() {
	h.t.Helper()

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), quietLogger()))
	h.t.Cleanup(cancel)

	client := h.sb.Client()
	startPipeline(ctx, h.cfg, sqlite.NewDownloadRepository(h.db, lease), client, client, h.arr)

	return func() {
		cancel()
		// Let the goroutines observe the cancellation before the next run starts,
		// so the old process cannot act on anything the new one is meant to.
		time.Sleep(5 * tick)
	}
}

// fakeTransferID is the id the fake seedbox gives its one transfer.
const fakeTransferID = "1"

func (h *harness) statusOf(id string) string {
	records, err := h.repo.GetDownloads()
	require.NoError(h.t, err)

	for _, r := range records {
		if r.DownloadID == id {
			return r.Status
		}
	}

	return ""
}

func (h *harness) waitForStatus(want string) {
	h.t.Helper()
	h.waitForStatusOf(fakeTransferID, want)
}

func (h *harness) waitForStatusOf(id, want string) {
	h.t.Helper()

	var last string

	ok := assert.Eventually(h.t, func() bool {
		last = h.statusOf(id)

		return last == want
	}, settle, tick)

	require.True(h.t, ok, "the transfer never reached %q in the ledger (last seen: %q)", want, last)
}

// waitForCleanup asserts the end state of the whole lifecycle: nothing left on
// Put.io, and the ledger says so, so the next restart will not resume it again.
func (h *harness) waitForCleanup(sb *seedbox.Seedbox) {
	h.t.Helper()

	require.Eventually(h.t, func() bool { return !sb.HasTransfer(transferName) }, settle, tick,
		"the transfer was never removed from Put.io")
	require.False(h.t, sb.HasFile(transferName), "the transfer's files must be deleted from Put.io too")

	h.waitForStatus(storage.StatusCleaned)
}

func newSeedbox(t *testing.T) *seedbox.Seedbox {
	t.Helper()

	return seedbox.New(t, "itv", seedbox.Transfer{
		Name:   transferName,
		Status: "SEEDING",
		Root: seedbox.Entry{
			Name:     transferName,
			Children: []seedbox.Entry{{Name: "episode.mkv", Content: content}},
		},
	})
}

// fakeArr answers the import check: imported or not, switchable mid-test.
type fakeArr struct {
	server   *httptest.Server
	imported atomic.Bool
}

func newFakeArr(t *testing.T) *fakeArr {
	t.Helper()

	f := &fakeArr{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if !f.imported.Load() {
			fmt.Fprint(w, `{"page":1,"pageSize":1,"totalRecords":0,"records":[]}`)

			return
		}

		// The client filters by download id server-side and checks only the event
		// type of what comes back, so the id in the record is immaterial.
		fmt.Fprint(w, `{"page":1,"pageSize":1,"totalRecords":1,"records":[
			{"eventType":"downloadFolderImported","downloadId":"ANY","data":{}}]}`)
	}))
	t.Cleanup(f.server.Close)

	return f
}

// quietLogger discards output unless TEST_LOG is set, for when a lifecycle
// test needs debugging.
func quietLogger() *slog.Logger {
	if os.Getenv("TEST_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
