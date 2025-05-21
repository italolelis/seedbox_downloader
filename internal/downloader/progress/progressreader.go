package progress

import "io"

// ProgressReader wraps an io.Reader and reports progress via a callback.
type ProgressReader struct {
	Reader         io.Reader
	Total          int64
	OnProgress     func(written int64, total int64)
	totalRead      int64 // cumulative total
	lastReport     int64 // bytes since last report
	reportInterval int64 // bytes
}

func NewReader(r io.Reader, total int64, interval int64, cb func(written int64, total int64)) *ProgressReader {
	return &ProgressReader{
		Reader:         r,
		Total:          total,
		OnProgress:     cb,
		reportInterval: interval,
	}
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
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
