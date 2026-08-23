package arr_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/svc/arr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeArr records what was asked of it, so the tests can assert on the shape of
// the query rather than only on the answer.
type fakeArr struct {
	server  *httptest.Server
	calls   atomic.Int32
	queries []url.Values
	body    string
}

func newFakeArr(t *testing.T, body string) *fakeArr {
	t.Helper()

	f := &fakeArr{body: body}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.queries = append(f.queries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, f.body)
	}))

	t.Cleanup(f.server.Close)

	return f
}

func (f *fakeArr) client() *arr.Client { return arr.NewClient("test-key", f.server.URL) }

const importedBody = `{"page":1,"pageSize":1,"totalRecords":1,"records":[
  {"eventType":"downloadFolderImported","downloadId":"ABC123","data":{}}]}`

const emptyBody = `{"page":1,"pageSize":1,"totalRecords":0,"records":[]}`

func TestCheckImported_True(t *testing.T) {
	f := newFakeArr(t, importedBody)

	got, err := f.client().CheckImported(context.Background(), "abc123")

	require.NoError(t, err)
	assert.True(t, got)
}

func TestCheckImported_False(t *testing.T) {
	f := newFakeArr(t, emptyBody)

	got, err := f.client().CheckImported(context.Background(), "abc123")

	require.NoError(t, err)
	assert.False(t, got)
}

// The whole point of the change: the history is filtered server-side, so exactly
// one request is made and it never pages. Walking the history cost the *arr app a
// release-title parse per record returned, which grew without bound.
func TestCheckImported_MakesOneFilteredRequestAndNeverPages(t *testing.T) {
	f := newFakeArr(t, importedBody)

	_, err := f.client().CheckImported(context.Background(), "abc123")
	require.NoError(t, err)

	require.EqualValues(t, 1, f.calls.Load(), "must not page through the history")
	require.Len(t, f.queries, 1)

	q := f.queries[0]
	assert.Equal(t, "ABC123", q.Get("downloadId"),
		"the *arr apps store the hash upper-cased and the filter is case-sensitive")
	assert.Equal(t, "3", q.Get("eventType"), "downloadFolderImported in both Sonarr and Radarr")
	assert.Equal(t, "1", q.Get("pageSize"), "only existence is needed")
	assert.Empty(t, q.Get("page"), "no paging")
}

// A record for the right download but the wrong event must not count as imported.
func TestCheckImported_IgnoresOtherEventTypes(t *testing.T) {
	f := newFakeArr(t, `{"totalRecords":1,"records":[{"eventType":"grabbed","downloadId":"ABC123","data":{}}]}`)

	got, err := f.client().CheckImported(context.Background(), "abc123")

	require.NoError(t, err)
	assert.False(t, got,
		"matching on the event name fails closed if either app renumbers its enum")
}

// An unconfigured client is still constructed for whichever *arr app is unused, so
// it has to answer without dialling anything.
func TestCheckImported_UnconfiguredClientMakesNoRequest(t *testing.T) {
	for _, tt := range []struct{ name, key, base string }{
		{"no base url", "key", ""},
		{"no api key", "", "http://example.invalid"},
		{"neither", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := arr.NewClient(tt.key, tt.base)

			assert.False(t, c.Configured())

			got, err := c.CheckImported(context.Background(), "abc123")
			require.NoError(t, err)
			assert.False(t, got)
		})
	}
}

func TestCheckImported_EmptyDownloadIDMakesNoRequest(t *testing.T) {
	f := newFakeArr(t, importedBody)

	got, err := f.client().CheckImported(context.Background(), "")

	require.NoError(t, err)
	assert.False(t, got)
	assert.EqualValues(t, 0, f.calls.Load())
}

func TestCheckImported_SurfacesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := arr.NewClient("bad-key", srv.URL).CheckImported(context.Background(), "abc123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
