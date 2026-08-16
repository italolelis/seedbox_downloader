// Package seedbox provides a fake Put.io for integration tests: a real HTTP
// server that speaks enough of the Put.io API for the real Put.io client and the
// real downloader to run against it, unmocked, writing to a real directory.
//
// It exists because the behaviour worth testing spans modules. The path written
// to disk is decided by the seedbox client and the downloader, while the path
// advertised to Sonarr/Radarr is decided by the RPC adapter -- so no single-module
// seam can assert the one invariant that matters: that those two agree.
package seedbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/dc/putio"
)

// Entry is one node in the fake account's file tree.
type Entry struct {
	// Name is the entry's own name, not a path. For a single-file transfer this
	// is the whole story; for a folder transfer the children hang off it.
	Name string

	// Content is the bytes served for a file. Ignored for folders.
	Content string

	// Children makes this entry a folder.
	Children []Entry

	// Status, when non-zero, is served instead of Content when this file is
	// fetched -- for exercising the rejection of error responses.
	Status int

	// TruncateTo, when positive, serves only that many bytes of Content in an
	// otherwise complete, well-formed response, while the file metadata still
	// reports the full size. Reading it produces no error -- the shortfall is only
	// visible by counting bytes against the reported size.
	TruncateTo int

	// AbortAfter, when positive, declares the full length and then hangs up after
	// that many bytes, so the read itself fails mid-stream. This is a different
	// failure from TruncateTo: here the copy errors, which is what exercises the
	// download-failure path rather than the silent-corruption path.
	AbortAfter int
}

func (e Entry) isDir() bool { return e.Children != nil }

// Transfer is one transfer on the fake account.
type Transfer struct {
	// Name is the transfer's own name. Deliberately allowed to differ from Root's
	// name -- that divergence is the entire subject of the tests using this.
	Name string

	// Root is the file or folder the transfer was saved as.
	Root Entry

	// Status defaults to COMPLETED when empty.
	Status string

	// InProgress omits the file id, as Put.io does before a transfer completes,
	// so no file details are discoverable yet.
	InProgress bool
}

// Seedbox is a running fake Put.io.
type Seedbox struct {
	t     *testing.T
	srv   *httptest.Server
	label string

	// nodes maps a synthetic file id to its entry and full path within the account.
	nodes     map[int64]*node
	children  map[int64][]int64
	transfers []fakeTransfer
	labelID   int64
	nextID    int64
}

type node struct {
	entry Entry
	id    int64
}

type fakeTransfer struct {
	id     int64
	name   string
	fileID int64
	status string
	size   int64
}

// New starts a fake Put.io serving the given transfers under the given label,
// and returns it. The server is shut down when the test finishes.
//
// The label is modelled the way Put.io does it: a folder on the account whose
// name is the label, with each transfer's root saved inside it.
func New(t *testing.T, label string, transfers ...Transfer) *Seedbox {
	t.Helper()

	s := &Seedbox{
		t:        t,
		label:    label,
		nodes:    map[int64]*node{},
		children: map[int64][]int64{},
		nextID:   100,
	}

	// The label folder itself, which the client resolves to decide whether a
	// transfer belongs to this instance.
	s.labelID = s.add(Entry{Name: label, Children: []Entry{}}, 0)

	for i, tr := range transfers {
		rootID := s.add(tr.Root, s.labelID)

		status := tr.Status
		if status == "" {
			status = "COMPLETED"
		}

		ft := fakeTransfer{
			id:     int64(i + 1),
			name:   tr.Name,
			fileID: rootID,
			status: status,
			size:   s.sizeOf(rootID),
		}

		if tr.InProgress {
			ft.fileID = 0
		}

		s.transfers = append(s.transfers, ft)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/transfers/list", s.handleTransfersList)
	mux.HandleFunc("/v2/files/list", s.handleFilesList)
	mux.HandleFunc("/v2/files/", s.handleFiles)
	mux.HandleFunc("/download/", s.handleDownload)

	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)

	return s
}

// Client returns a real Put.io client pointed at this fake.
func (s *Seedbox) Client() *putio.Client {
	s.t.Helper()

	u, err := url.Parse(s.srv.URL)
	if err != nil {
		s.t.Fatalf("parsing fake seedbox URL: %v", err)
	}

	return putio.NewClient("test-token", putio.WithBaseURL(u))
}

// Label is the label the fake's transfers are filed under.
func (s *Seedbox) Label() string { return s.label }

// DownloadURL is the URL the fake serves a file's bytes from. Exposed so tests
// can assert on the fake's own behaviour without going through a client.
func (s *Seedbox) DownloadURL(fileID int64) string {
	return fmt.Sprintf("%s/download/%d", s.srv.URL, fileID)
}

// add registers an entry and its descendants, returning the new id.
func (s *Seedbox) add(e Entry, parentID int64) int64 {
	s.nextID++
	id := s.nextID
	s.nodes[id] = &node{entry: e, id: id}

	if parentID != 0 {
		s.children[parentID] = append(s.children[parentID], id)
	}

	for _, child := range e.Children {
		s.add(child, id)
	}

	return id
}

func (s *Seedbox) sizeOf(id int64) int64 {
	n := s.nodes[id]
	if !n.entry.isDir() {
		return int64(len(n.entry.Content))
	}

	var total int64
	for _, childID := range s.children[id] {
		total += s.sizeOf(childID)
	}

	return total
}

func (s *Seedbox) handleTransfersList(w http.ResponseWriter, _ *http.Request) {
	out := make([]map[string]any, 0, len(s.transfers))

	for _, tr := range s.transfers {
		out = append(out, map[string]any{
			"id":             tr.id,
			"name":           tr.name,
			"file_id":        tr.fileID,
			"save_parent_id": s.labelID,
			"status":         tr.status,
			"percent_done":   100,
			"size":           tr.size,
			"downloaded":     tr.size,
			"source":         "magnet:test",
		})
	}

	writeJSON(w, map[string]any{"transfers": out})
}

func (s *Seedbox) handleFilesList(w http.ResponseWriter, r *http.Request) {
	parentID, err := strconv.ParseInt(r.URL.Query().Get("parent_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad parent_id", http.StatusBadRequest)

		return
	}

	parent, ok := s.nodes[parentID]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	out := make([]map[string]any, 0, len(s.children[parentID]))
	for _, childID := range s.children[parentID] {
		out = append(out, s.fileJSON(childID))
	}

	writeJSON(w, map[string]any{
		"files":  out,
		"parent": s.fileJSON(parent.id),
		"cursor": "",
	})
}

// handleFiles serves both /v2/files/{id} and /v2/files/{id}/url.
func (s *Seedbox) handleFiles(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v2/files/")

	idPart, isURL := rest, false
	if suffix := "/url"; strings.HasSuffix(rest, suffix) {
		idPart, isURL = strings.TrimSuffix(rest, suffix), true
	}

	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		http.Error(w, "bad file id", http.StatusBadRequest)

		return
	}

	if _, ok := s.nodes[id]; !ok {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	if isURL {
		writeJSON(w, map[string]any{"url": fmt.Sprintf("%s/download/%d", s.srv.URL, id)})

		return
	}

	writeJSON(w, map[string]any{"file": s.fileJSON(id)})
}

// handleDownload serves file bytes, honouring the entry's injected failures.
func (s *Seedbox) handleDownload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/download/"), 10, 64)
	if err != nil {
		http.Error(w, "bad file id", http.StatusBadRequest)

		return
	}

	n, ok := s.nodes[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)

		return
	}

	if n.entry.Status != 0 {
		http.Error(w, "the seedbox is unhappy", n.entry.Status)

		return
	}

	body := n.entry.Content

	if n.entry.AbortAfter > 0 && n.entry.AbortAfter < len(body) {
		// Promising more than we deliver makes the client's read fail with an
		// unexpected EOF, which is a genuine mid-stream transfer failure.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(body[:n.entry.AbortAfter])); err != nil {
			return
		}

		return
	}

	if n.entry.TruncateTo > 0 && n.entry.TruncateTo < len(body) {
		// Deliberately do NOT set Content-Length to the full size. Doing so would
		// make Go's HTTP client report an unexpected EOF, which the copy already
		// surfaces -- a truncation that is caught for free is not the interesting
		// case. Serving a complete, well-formed short response makes the only
		// evidence of truncation the mismatch against the size the seedbox reported
		// in its metadata, which is the silent case that has to be caught by
		// counting bytes.
		body = body[:n.entry.TruncateTo]
	}

	if _, err := w.Write([]byte(body)); err != nil {
		return
	}
}

func (s *Seedbox) fileJSON(id int64) map[string]any {
	n := s.nodes[id]

	fileType, contentType := "VIDEO", "video/x-matroska"
	if n.entry.isDir() {
		fileType, contentType = "FOLDER", "application/x-directory"
	}

	return map[string]any{
		"id":           n.id,
		"name":         n.entry.Name,
		"size":         s.sizeOf(id),
		"file_type":    fileType,
		"content_type": contentType,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
	}
}
