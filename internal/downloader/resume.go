package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
)

// maxStalledResumes is how many resume attempts in a row may fail to add a
// single byte to the file before the download is given up. A resume that does
// add bytes starts a fresh budget, so a long file can survive any number of
// cut connections as long as each one gets it further.
const maxStalledResumes = 5

// Option configures a Downloader.
type Option func(*Downloader)

// WithResumeBackOff sets the wait between attempts to resume an interrupted
// download. newBackOff is called once per run of stalled attempts.
func WithResumeBackOff(newBackOff func() backoff.BackOff) Option {
	return func(d *Downloader) {
		d.resumeBackOff = newBackOff
	}
}

func defaultResumeBackOff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 2 * time.Second
	b.MaxInterval = time.Minute

	return b
}

// sourceError marks a failure reading the download's body, as opposed to
// writing the file. Only the former is worth resuming: the remote end hung up,
// while a write failure (a full disk) happens again on every attempt.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// sourceReader marks its reader's errors as sourceErrors. io.EOF is passed
// through untouched, because io.Copy compares it by identity to detect a body
// that ended cleanly.
type sourceReader struct{ r io.Reader }

func (s sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF { //nolint:errorlint // io.Copy needs io.EOF itself, not a wrapped one.
		err = &sourceError{err: err}
	}

	return n, err
}

// resumable reports whether a failed copy can be picked up where it left off:
// the body broke off before the file's known end, and nobody asked us to stop.
func resumable(ctx context.Context, err error, written, size int64) bool {
	var se *sourceError

	return ctx.Err() == nil && errors.As(err, &se) && size > 0 && written < size
}

// resumeFile continues a download whose body ended before the file did -- a
// long download cut off by the server, a CDN or an expiring URL -- from the
// bytes already on disk, so a file that takes longer to fetch than whatever cuts
// the connection can still complete. It returns the file's final length.
func (d *Downloader) resumeFile(
	ctx context.Context, logger *slog.Logger, out *os.File, file *transfer.File, targetPath string, written int64, cause error,
) (int64, error) {
	attempt := 0

	for written < file.Size {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		_, err := backoff.Retry(ctx, func() (struct{}, error) {
			attempt++
			start := written

			logger.InfoContext(ctx, "resuming interrupted download",
				"file_path", file.Path,
				"offset", start,
				"size", file.Size,
				"attempt", attempt,
				"cause", cause)

			var err error

			written, err = d.continueFile(ctx, logger, out, file, targetPath, start)
			if err == nil {
				return struct{}{}, nil
			}

			cause = err

			var permanent *backoff.PermanentError
			if errors.As(err, &permanent) {
				return struct{}{}, err
			}

			// Bytes were added, so this connection was worth having: hand back to
			// the outer loop, which resumes at once with a fresh budget.
			if written > start {
				return struct{}{}, nil
			}

			return struct{}{}, err
		},
			backoff.WithBackOff(d.resumeBackOff()),
			backoff.WithMaxTries(maxStalledResumes),
			// Elapsed time is not the limit: a stalled attempt fails fast, and one
			// that copies for an hour has made progress and ends this run anyway.
			backoff.WithMaxElapsedTime(0))
		if err != nil {
			return written, fmt.Errorf("giving up resuming %s at byte %d of %d after %d attempts: %w",
				file.Path, written, file.Size, attempt, err)
		}
	}

	return written, nil
}

// continueFile makes one attempt to fetch the rest of a file from offset onto
// out, and returns how long the file now is. Errors that another attempt cannot
// fix are marked permanent.
func (d *Downloader) continueFile(
	ctx context.Context, logger *slog.Logger, out *os.File, file *transfer.File, targetPath string, offset int64,
) (int64, error) {
	body, err := d.dc.GrabFileRange(ctx, file, offset)
	if errors.Is(err, transfer.ErrRangeNotHonoured) {
		// Appending a body that does not start at offset would corrupt the file,
		// so the only safe continuation is the whole file again.
		logger.WarnContext(ctx, "seedbox would not resume from an offset, restarting file from the beginning",
			"file_path", file.Path, "offset", offset, "err", err)

		offset = 0
		body, err = d.dc.GrabFile(ctx, file)

		if err == nil {
			if err := out.Truncate(0); err != nil {
				body.Close()

				return 0, backoff.Permanent(fmt.Errorf("failed to truncate %s to restart it: %w", targetPath, err))
			}
		}
	}

	if err != nil {
		// A file gone from the seedbox stays gone; anything else (a refused
		// connection, a 5xx) may well pass.
		if errors.Is(err, putio.ErrTransferFilesNotFound) {
			return offset, backoff.Permanent(err)
		}

		return offset, fmt.Errorf("failed to grab file from byte %d: %w", offset, err)
	}

	defer body.Close()

	// The file position already equals offset after a read error, because io.Copy
	// writes every byte it read before reporting one. Seeking makes that explicit
	// rather than an assumption, and is required after a truncate.
	if _, err := out.Seek(offset, io.SeekStart); err != nil {
		return offset, backoff.Permanent(fmt.Errorf("failed to seek %s to byte %d: %w", targetPath, offset, err))
	}

	n, err := d.writeFile(ctx, out, body, file.Path, targetPath, offset, file.Size)
	if err != nil {
		var se *sourceError
		if !errors.As(err, &se) {
			return offset + n, backoff.Permanent(err)
		}

		return offset + n, err
	}

	return offset + n, nil
}
