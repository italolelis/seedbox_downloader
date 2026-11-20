package progress

import "io"

// Reader wraps an io.Reader and reports progress via a callback.
type Reader struct {
	Reader         io.Reader
	Total          int64
	OnProgress     func(written int64, total int64)
	totalRead      int64 // cumulative total
	lastReport     int64 // bytes since last report
	reportInterval int64 // bytes
}

func NewReader(r io.Reader, total int64, interval int64, cb func(written int64, total int64)) *Reader {
	return NewReaderFrom(r, 0, total, interval, cb)
}

// NewReaderFrom is NewReader for a body that continues a file from offset, so
// progress reports the file's true position rather than restarting at zero
// each time a download is resumed.
func NewReaderFrom(r io.Reader, offset, total int64, interval int64, cb func(written int64, total int64)) *Reader {
	return &Reader{
		Reader:         r,
		Total:          total,
		OnProgress:     cb,
		totalRead:      offset,
		reportInterval: interval,
	}
}

func (pr *Reader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.totalRead += int64(n)
		pr.lastReport += int64(n)

		if pr.lastReport >= pr.reportInterval || (pr.Total > 0 && pr.totalRead*100/pr.Total >= 5 && (pr.totalRead-int64(n))*100/pr.Total < 5) {
			pr.OnProgress(pr.totalRead, pr.Total)
			pr.lastReport = 0
		}
	}

	return n, err
}
