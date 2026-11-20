package downloader

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestSyncWriterSyncsEachTimeTheThresholdIsReached(t *testing.T) {
	var (
		buf   bytes.Buffer
		syncs int
	)

	w := newSyncWriter(&buf, func() error { syncs++; return nil }, 10)

	// 33 bytes in 4-byte writes against a threshold of 10: pending reaches 12
	// at byte 12 (sync), 12 again at byte 24 (sync), then ends at 9, below the
	// threshold, so it waits for the next write or the close.
	src := strings.Repeat("x", 33)
	for chunk := range slices.Chunk([]byte(src), 4) {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if buf.String() != src {
		t.Fatalf("wrote %d bytes, want %d", buf.Len(), len(src))
	}

	if syncs != 2 {
		t.Fatalf("synced %d times, want 2 (after bytes 12 and 24; 9 left pending)", syncs)
	}
}

func TestSyncWriterReturnsTheSyncError(t *testing.T) {
	errDiskFull := errors.New("no space left on device")

	w := newSyncWriter(io.Discard, func() error { return errDiskFull }, 1)

	n, err := w.Write([]byte("abc"))
	if !errors.Is(err, errDiskFull) {
		t.Fatalf("err = %v, want %v", err, errDiskFull)
	}

	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}
}
