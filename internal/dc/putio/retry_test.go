package putio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/putdotio/go-putio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMaxTries = 3

// scriptedAPI is a fake Put.io whose endpoints fail with the scripted statuses,
// in order, before answering normally. It counts every call, so a test can tell
// a retried failure from one returned on the first try.
type scriptedAPI struct {
	mu     sync.Mutex
	script map[string][]int
	calls  map[string]int
	bodies map[string]string
}

func newScriptedAPI(t *testing.T, bodies map[string]string, script map[string][]int) (*scriptedAPI, *Client) {
	t.Helper()

	api := &scriptedAPI{script: script, calls: map[string]int{}, bodies: bodies}

	srv := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	return api, NewClient("test-token", WithBaseURL(u), WithRetry(testMaxTries, time.Millisecond))
}

func (a *scriptedAPI) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	path := r.URL.Path
	a.calls[path]++

	if statuses := a.script[path]; len(statuses) > 0 {
		a.script[path] = statuses[1:]

		if statuses[0] == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statuses[0])
		fmt.Fprintf(w, `{"error_type":%q,"error_message":"scripted"}`, http.StatusText(statuses[0]))

		return
	}

	body, ok := a.bodies[path]
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error_type":"NotFound","error_message":"not found"}`)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func (a *scriptedAPI) callsTo(path string) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.calls[path]
}

func repeat(status, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = status
	}

	return out
}

// Only Put.io saying the file does not exist may report it missing. A 429 used
// to be reported that way, which marked a transfer of 549 present files as
// deleted.
func TestGrabFile_ClassifiesURLFailures(t *testing.T) {
	const urlPath = "/v2/files/7/url"

	tests := []struct {
		name        string
		script      []int
		wantOK      bool
		wantMissing bool
		wantCalls   int
	}{
		{name: "rate limit then success is retried", script: []int{429, 429}, wantOK: true, wantCalls: 3},
		{name: "server error then success is retried", script: []int{503}, wantOK: true, wantCalls: 2},
		{name: "sustained rate limit is an error, not missing", script: repeat(429, 10), wantCalls: testMaxTries},
		{name: "404 is missing, without retrying", script: []int{404}, wantMissing: true, wantCalls: 1},
		{name: "410 is missing, without retrying", script: []int{410}, wantMissing: true, wantCalls: 1},
		{name: "403 is an error, not missing, without retrying", script: []int{403}, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, client := newScriptedAPI(t, map[string]string{}, map[string][]int{urlPath: tt.script})

			// The download URL points back at the same fake, at a path serving the bytes.
			srvURL := strings.TrimSuffix(client.putioClient.BaseURL.String(), "/")

			api.mu.Lock()
			api.bodies[urlPath] = fmt.Sprintf(`{"url":%q}`, srvURL+"/content/7")
			api.bodies["/content/7"] = "bytes"
			api.mu.Unlock()

			body, err := client.GrabFile(context.Background(), &transfer.File{ID: 7, Path: "a.mkv"})

			assert.Equal(t, tt.wantCalls, api.callsTo(urlPath))

			if tt.wantOK {
				require.NoError(t, err)
				body.Close()

				return
			}

			require.Error(t, err)
			assert.Equal(t, tt.wantMissing, errors.Is(err, ErrTransferFilesNotFound), "missing classification: %v", err)
		})
	}
}

// A sibling download failing cancels the context of every other in-flight one.
// Those cancellations are the caller giving up, not evidence about the file.
func TestGrabFile_CancelledContextIsNotMissing(t *testing.T) {
	_, client := newScriptedAPI(t, map[string]string{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GrabFile(ctx, &transfer.File{ID: 7, Path: "a.mkv"})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrTransferFilesNotFound)
}

func TestGetTaggedTorrents_FileLookupFailures(t *testing.T) {
	const transfers = `{"transfers":[{"id":1,"name":"show","file_id":100,"save_parent_id":200,"status":"COMPLETED"}]}`

	bodies := map[string]string{
		"/v2/transfers/list": transfers,
		"/v2/files/200":      `{"file":{"id":200,"name":"mytag","file_type":"FOLDER","content_type":"application/x-directory"}}`,
		"/v2/files/100":      `{"file":{"id":100,"name":"show.mkv","size":10,"file_type":"VIDEO"}}`,
	}

	tests := []struct {
		name      string
		script    map[string][]int
		wantCount int
		wantFiles int
	}{
		{
			name:      "rate limited lookups are retried and the files listed",
			script:    map[string][]int{"/v2/files/200": {429}, "/v2/files/100": {429, 429}},
			wantCount: 1,
			wantFiles: 1,
		},
		{
			name:      "files that are gone drop the transfer",
			script:    map[string][]int{"/v2/files/100": {404}},
			wantCount: 0,
		},
		{
			// Dropping it would tell the *arr apps it vanished; listed without
			// files, the pipeline skips it until a later poll can read them.
			name:      "a sustained rate limit keeps the transfer listed without files",
			script:    map[string][]int{"/v2/files/100": repeat(429, 10)},
			wantCount: 1,
			wantFiles: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := make(map[string]string, len(bodies))
			for k, v := range bodies {
				b[k] = v
			}

			_, client := newScriptedAPI(t, b, tt.script)

			torrents, err := client.GetTaggedTorrents(context.Background(), "mytag")
			require.NoError(t, err)
			require.Len(t, torrents, tt.wantCount)

			if tt.wantCount > 0 {
				assert.Len(t, torrents[0].Files, tt.wantFiles)
			}
		})
	}
}

// A nested folder that cannot be read must fail the listing of the transfer's
// files, not be skipped: a partial list would be downloaded and reported
// complete with a folder missing.
func TestGetTaggedTorrents_UnreadableSubfolderLeavesNoPartialList(t *testing.T) {
	bodies := map[string]string{
		"/v2/transfers/list": `{"transfers":[{"id":1,"name":"season","file_id":100,"save_parent_id":200,"status":"COMPLETED"}]}`,
		"/v2/files/200":      `{"file":{"id":200,"name":"mytag","file_type":"FOLDER","content_type":"application/x-directory"}}`,
		"/v2/files/100":      `{"file":{"id":100,"name":"season","file_type":"FOLDER","content_type":"application/x-directory"}}`,
		"/v2/files/101":      `{"file":{"id":101,"name":"extras","file_type":"FOLDER","content_type":"application/x-directory"}}`,
		"/v2/files/list": `{"files":[` +
			`{"id":102,"name":"e01.mkv","size":1,"file_type":"VIDEO"},` +
			`{"id":101,"name":"extras","file_type":"FOLDER","content_type":"application/x-directory"}]}`,
	}

	_, client := newScriptedAPI(t, bodies, map[string][]int{"/v2/files/101": repeat(500, 10)})

	torrents, err := client.GetTaggedTorrents(context.Background(), "mytag")
	require.NoError(t, err)
	require.Len(t, torrents, 1)
	assert.Empty(t, torrents[0].Files)
}

func TestGetTaggedTorrents_CancelledContextIsAnError(t *testing.T) {
	_, client := newScriptedAPI(t, map[string]string{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetTaggedTorrents(ctx, "mytag")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRetryAfter(t *testing.T) {
	errWith := func(header string) error {
		h := http.Header{}
		if header != "" {
			h.Set("Retry-After", header)
		}

		return &putio.ErrorResponse{Response: &http.Response{StatusCode: http.StatusTooManyRequests, Header: h}}
	}

	tests := []struct {
		name   string
		err    error
		want   time.Duration
		wantOK bool
	}{
		{name: "seconds", err: errWith("7"), want: 7 * time.Second, wantOK: true},
		{name: "date in the past waits nothing", err: errWith("Mon, 02 Jan 2006 15:04:05 GMT"), want: 0, wantOK: true},
		{name: "no header", err: errWith("")},
		{name: "garbage", err: errWith("soon")},
		{name: "not a Put.io response", err: errors.New("connection reset")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := retryAfter(tt.err)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
