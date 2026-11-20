package downloader

import (
	"fmt"
	"io"
)

// syncEvery is how many bytes a download may write before they are flushed to
// stable storage.
//
// Without it, nothing flushes until the file is closed. On a network
// filesystem the seedbox can deliver bytes faster than the server accepts
// them, so dirty and writeback pages pile up in the page cache. Those pages
// count against the container's memory limit and cannot be reclaimed until the
// server acknowledges them, so one large file was enough to OOM-kill a process
// whose own heap was 9 MB. Flushing every syncEvery bytes caps that backlog at
// roughly this size, whatever the limit or the link speed.
const syncEvery = 64 << 20 // 64 MiB

// syncWriter writes to w and calls sync each time at least every bytes have
// been written since the last sync.
type syncWriter struct {
	w       io.Writer
	sync    func() error
	every   int64
	pending int64
}

func newSyncWriter(w io.Writer, sync func() error, every int64) *syncWriter {
	return &syncWriter{w: w, sync: sync, every: every}
}

func (s *syncWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	s.pending += int64(n)

	if err != nil {
		return n, err
	}

	if s.pending >= s.every {
		s.pending = 0

		if err := s.sync(); err != nil {
			return n, fmt.Errorf("failed to sync written data: %w", err)
		}
	}

	return n, nil
}
