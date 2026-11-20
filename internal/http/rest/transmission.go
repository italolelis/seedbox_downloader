package rest

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/storage"
	"github.com/italolelis/seedbox_downloader/internal/telemetry"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/zeebo/bencode"
)

const sessionID = "useless-session-id"

const maxTorrentSize = 10 * 1024 * 1024 // 10MB - matches Phase 4 limit

// categoryDirPerm matches the permissions the downloader creates directories with.
const categoryDirPerm = 0o755

// DownloadClient defines the interface for torrent client operations.
// This interface enables mocking in tests while the production code uses *putio.Client.
type DownloadClient interface {
	AddTransfer(ctx context.Context, magnetLink, label, category string) (*transfer.Transfer, error)
	AddTransferByBytes(ctx context.Context, content []byte, filename, label, category string) (*transfer.Transfer, error)
	GetTaggedTorrents(ctx context.Context, label string) ([]*transfer.Transfer, error)
	RemoveTransfers(ctx context.Context, ids []string, deleteData bool) error
}

// TransferLedger is the downloader's own record of where each transfer is in its
// lifecycle (see storage.IsLocal). The handler reads it because the seedbox can
// only say a transfer is complete on the seedbox; whether its content is on local
// disk yet -- which is all an *arr app cares about -- only the ledger knows.
type TransferLedger interface {
	TransferStatuses(transferIDs []string) (map[string]string, error)
}

type TransmissionTorrentStatus int

const (
	StatusStopped TransmissionTorrentStatus = iota
	StatusCheckWait
	StatusCheck
	StatusDownloadWait
	StatusDownload
	StatusSeedWait
	StatusSeed
)

type TransmissionTorrent struct {
	ID                 int64                     `json:"id"`
	HashString         string                    `json:"hashString,omitempty"`
	Name               string                    `json:"name"`
	DownloadDir        string                    `json:"downloadDir"`
	TotalSize          int64                     `json:"totalSize"`
	LeftUntilDone      int64                     `json:"leftUntilDone"`
	IsFinished         bool                      `json:"isFinished"`
	ETA                int64                     `json:"eta"`
	Status             TransmissionTorrentStatus `json:"status"`
	SecondsDownloading int64                     `json:"secondsDownloading"`
	ErrorString        *string                   `json:"errorString,omitempty"`
	DownloadedEver     int64                     `json:"downloadedEver"`
	Labels             []string                  `json:"labels"`
	PeersConnected     int64                     `json:"peersConnected"`
	PeersSendingToUs   int64                     `json:"peersSendingToUs"`
	PeersGettingFromUs int64                     `json:"peersGettingFromUs"`
	RateDownload       int64                     `json:"rateDownload"`
	SeedRatioLimit     float32                   `json:"seedRatioLimit"`
	SeedRatioMode      uint32                    `json:"seedRatioMode"`
	SeedIdleLimit      uint64                    `json:"seedIdleLimit"`
	SeedIdleMode       uint32                    `json:"seedIdleMode"`
	FileCount          uint32                    `json:"fileCount"`
}

type TransmissionResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

type TransmissionRequest struct {
	Method    string `json:"method"`
	Arguments struct {
		Fields          []string `json:"fields"`
		IDs             []string `json:"ids"`
		Format          string   `json:"format"`
		FileName        string   `json:"filename"`
		Paused          bool     `json:"paused"`
		DownloadDir     string   `json:"download-dir"`
		Labels          []string `json:"labels"`
		MetaInfo        string   `json:"metainfo"`
		SeedRationLimit float64  `json:"seedRatioLimit"`
		SeedRatioMode   int64    `json:"seedRatioMode"`
		SeedIdleLimit   int64    `json:"seedIdleLimit"`
		SeedIdleMode    int64    `json:"seedIdleMode"`
		DeleteLocalData bool     `json:"delete-local-data"`
	} `json:"arguments"`
}

type TransmissionConfig struct {
	RPCVersion              string  `json:"rpc-version"`
	Version                 string  `json:"version"`
	DownloadDir             string  `json:"download-dir"`
	SeedRatioLimit          float32 `json:"seedRatioLimit"`
	SeedRatioLimited        bool    `json:"seedRatioLimited"`
	IdleSeedingLimit        uint64  `json:"idle-seeding-limit"`
	IdleSeedingLimitEnabled bool    `json:"idle-seeding-limit-enabled"`
}

func NewTransmissionConfig(downloadDir string) *TransmissionConfig {
	return &TransmissionConfig{
		RPCVersion:              "18",
		Version:                 "14.0.0",
		DownloadDir:             downloadDir,
		SeedRatioLimit:          1.0,
		SeedRatioLimited:        true,
		IdleSeedingLimit:        100,
		IdleSeedingLimitEnabled: false,
	}
}

type TransmissionHandler struct {
	username string
	password string
	dc       DownloadClient
	// ledger decides when a transfer is reported complete. Nil means nothing is
	// ever known to be local, so nothing the seedbox has finished is reported
	// complete: the safe failure, since reporting a partial file complete gets it
	// imported into the library.
	ledger TransferLedger
	label  string
	// localRoot is the directory on local disk that transfers are written into.
	// It is what gets advertised to the *arr apps, because it is the only path
	// that actually exists from their point of view. A seedbox-side path must
	// never be put here: advertising one is why imports silently never happened.
	localRoot string
	telemetry *telemetry.Telemetry
}

// NewTransmissionHandler creates a new content handler.
func NewTransmissionHandler(
	username, password string, dc DownloadClient, ledger TransferLedger, label string, localRoot string, t *telemetry.Telemetry,
) *TransmissionHandler {
	return &TransmissionHandler{
		username:  username,
		password:  password,
		dc:        dc,
		ledger:    ledger,
		label:     label,
		localRoot: localRoot,
		telemetry: t,
	}
}

func (h *TransmissionHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.basicAuthMiddleware)

	r.Post("/transmission/rpc", h.HandleRPC)
	r.Get("/transmission/rpc", h.HandleRPCGet)

	return r
}

// HandleRPC responsible to receive the callback from a webhook.
func (h *TransmissionHandler) HandleRPC(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logctx.LoggerFromContext(ctx)
	logger.DebugContext(ctx, "received post rpc request")

	var req TransmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "failed to decode request", "err", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)

		return
	}

	var response *TransmissionResponse

	var err error

	switch req.Method {
	case "session-get":
		tConfig := NewTransmissionConfig(h.localRoot)

		w.Header().Set("Content-Type", "application/json")

		jsonConfig, err := json.Marshal(tConfig)
		if err != nil {
			logger.ErrorContext(ctx, "failed to marshal config", "err", err)
			http.Error(w, "failed to marshal config", http.StatusInternalServerError)

			return
		}

		response = &TransmissionResponse{
			Result:    "success",
			Arguments: jsonConfig,
		}
	case "torrent-get":
		response, err = h.handleTorrentGet(ctx)
	case "torrent-set":
		// Nothing to do here
		response = &TransmissionResponse{
			Result: "success",
		}
	case "queue-move-top":
		// Nothing to do here
		response = &TransmissionResponse{
			Result: "success",
		}
	case "torrent-remove":
		response, err = h.handleTorrentRemove(ctx, &req)
	case "torrent-add":
		response, err = h.handleTorrentAdd(ctx, &req)
	default:
		logger.ErrorContext(ctx, "unknown method", "method", req.Method)
		http.Error(w, fmt.Sprintf("unknown method %s", req.Method), http.StatusBadRequest)

		return
	}

	if err != nil {
		logger.ErrorContext(ctx, "failed to handle request", "method", req.Method, "err", err)

		// Transmission RPC returns HTTP 200 with error in result field
		// This allows clients to display specific error messages
		errorResponse := &TransmissionResponse{
			Result: formatTransmissionError(err),
		}

		w.Header().Set("Content-Type", "application/json")

		if encodeErr := json.NewEncoder(w).Encode(errorResponse); encodeErr != nil {
			// Only use HTTP error for server-side failures (encoding)
			logger.ErrorContext(ctx, "failed to encode error response", "err", encodeErr)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.ErrorContext(ctx, "failed to encode response", "err", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)

		return
	}
}

// HandleRPCGet handles GET requests to the RPC endpoint.
func (h *TransmissionHandler) HandleRPCGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Transmission-Session-Id", sessionID)
	w.WriteHeader(http.StatusConflict)
	w.Write([]byte("{}"))
}

func (h *TransmissionHandler) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "invalid authorization format", http.StatusUnauthorized)

			return
		}

		if username != h.username || password != h.password {
			http.Error(w, "invalid username or password", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// validateBencodeStructure validates that data is proper bencode torrent structure.
func validateBencodeStructure(data []byte) error {
	var torrentData interface{}

	// Decode bencode structure
	if err := bencode.DecodeBytes(data, &torrentData); err != nil {
		return &transfer.InvalidContentError{
			Filename: "metainfo",
			Reason:   fmt.Sprintf("invalid bencode structure: %v", err),
			Err:      err,
		}
	}

	// Verify root is a dictionary
	dict, ok := torrentData.(map[string]interface{})
	if !ok {
		return &transfer.InvalidContentError{
			Filename: "metainfo",
			Reason:   "bencode root must be a dictionary",
		}
	}

	// Check for required 'info' field
	if _, hasInfo := dict["info"]; !hasInfo {
		return &transfer.InvalidContentError{
			Filename: "metainfo",
			Reason:   "bencode missing required 'info' dictionary",
		}
	}

	return nil
}

// generateTorrentFilename generates a unique .torrent filename from torrent content.
func generateTorrentFilename(torrentBytes []byte) string {
	hash := sha1.Sum(torrentBytes)
	hashStr := hex.EncodeToString(hash[:])

	return fmt.Sprintf("%s.torrent", hashStr[:16])
}

// requestCategory is the category the requesting app asked for. Sonarr and Radarr
// send it as a label on torrent-add, taken from the client's Category setting:
//
//	if (settings.TvCategory.IsNotNullOrWhiteSpace())
//	    arguments.Add("labels", new List<string> { settings.TvCategory });
//
// Empty means the app has no category configured, in which case the transfer goes
// to the target label's own folder -- which is where everything went before
// categories were honoured at all.
func requestCategory(req *TransmissionRequest) string {
	for _, label := range req.Arguments.Labels {
		if label != "" {
			return label
		}
	}

	return ""
}

// handleTorrentAddByMetaInfo processes .torrent file content from MetaInfo field.
func (h *TransmissionHandler) handleTorrentAddByMetaInfo(ctx context.Context, req *TransmissionRequest) (*transfer.Transfer, error) {
	logger := logctx.LoggerFromContext(ctx)

	// Decode base64 content (requirement API-02)
	torrentBytes, err := base64.StdEncoding.DecodeString(req.Arguments.MetaInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to decode base64 metainfo",
			"err", err,
			"error_type", "invalid_base64",
			"metainfo_length", len(req.Arguments.MetaInfo),
		)

		return nil, &transfer.InvalidContentError{
			Filename: "metainfo",
			Reason:   fmt.Sprintf("invalid base64 encoding: %v", err),
			Err:      err,
		}
	}

	logger.DebugContext(ctx, "decoded metainfo", "size_bytes", len(torrentBytes))

	// Check size BEFORE bencode validation (prevent memory exhaustion)
	if len(torrentBytes) > maxTorrentSize {
		return nil, &transfer.InvalidContentError{
			Filename: "metainfo",
			Reason:   fmt.Sprintf("size %d bytes exceeds maximum %d bytes", len(torrentBytes), maxTorrentSize),
		}
	}

	// Validate bencode structure (requirement API-03)
	if err := validateBencodeStructure(torrentBytes); err != nil {
		logger.ErrorContext(ctx, "bencode validation failed",
			"err", err,
			"error_type", "invalid_bencode",
			"size_bytes", len(torrentBytes),
		)

		return nil, err
	}

	// Generate filename for Put.io (requires .torrent extension)
	filename := generateTorrentFilename(torrentBytes)
	logger.DebugContext(ctx, "generated filename", "filename", filename)

	// Upload to Put.io using Phase 4 client method
	torrent, err := h.dc.AddTransferByBytes(ctx, torrentBytes, filename, h.label, requestCategory(req))
	if err != nil {
		logger.ErrorContext(ctx, "failed to add transfer by bytes",
			"err", err,
			"error_type", "api_error",
			"filename", filename,
			"size_bytes", len(torrentBytes),
		)

		return nil, err
	}

	logger.InfoContext(ctx, "transfer created from metainfo", "transfer_id", torrent.ID, "name", torrent.Name)

	return torrent, nil
}

func (h *TransmissionHandler) handleTorrentAdd(ctx context.Context, req *TransmissionRequest) (*TransmissionResponse, error) {
	logger := logctx.LoggerFromContext(ctx).With("method", "handle_torrent_add")

	torrent, err := h.resolveTorrent(ctx, req, logger)
	if err != nil {
		return nil, err
	}

	h.ensureCategoryDir(ctx, logger, requestCategory(req))

	// Marshal success response (Transmission format with torrent-added)
	jsonTorrent, err := json.Marshal(map[string]interface{}{
		"torrent-added": map[string]interface{}{
			"id":         torrent.ID,
			"name":       torrent.Name,
			"hashString": torrent.ID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal torrent: %w", err)
	}

	return &TransmissionResponse{
		Result:    "success",
		Arguments: jsonTorrent,
	}, nil
}

// resolveTorrent determines the torrent source (metainfo or magnet) and adds the transfer.
func (h *TransmissionHandler) resolveTorrent(
	ctx context.Context, req *TransmissionRequest, logger *slog.Logger,
) (*transfer.Transfer, error) {
	// Requirement API-06: Prioritize MetaInfo when both present
	if req.Arguments.MetaInfo != "" {
		// Requirement API-01: Detect MetaInfo field
		logger.DebugContext(ctx, "processing torrent add request", "torrent_type", "metainfo")

		if h.telemetry != nil {
			h.telemetry.RecordTorrentType(ctx, "metainfo")
		}

		return h.handleTorrentAddByMetaInfo(ctx, req)
	}

	if req.Arguments.FileName != "" {
		// Requirement API-05: Maintain backward compatibility
		logger.DebugContext(ctx, "processing torrent add request", "torrent_type", "magnet")

		if h.telemetry != nil {
			h.telemetry.RecordTorrentType(ctx, "magnet")
		}

		magnetLink := req.Arguments.FileName
		// The target label scopes everything this instance owns on a possibly shared
		// account; the requesting app's category picks a folder within it.
		torrent, err := h.dc.AddTransfer(ctx, magnetLink, h.label, requestCategory(req))
		if err != nil {
			return nil, fmt.Errorf("failed to add transfer: %w", err)
		}

		return torrent, nil
	}

	return nil, fmt.Errorf("either metainfo or filename must be provided")
}

// ensureCategoryDir creates the local directory for a category the moment an app
// first asks for it.
//
// Sonarr and Radarr check that "download directory plus category" exists and warn
// that the client "places downloads in <path> but this directory does not appear
// to exist" when it doesn't. Waiting for the first completed download to create it
// would leave that warning showing for however long the first grab takes.
//
// Failure is logged, not returned: the transfer has already been accepted by the
// seedbox at this point, and the downloader creates the directory again when it
// writes anyway.
func (h *TransmissionHandler) ensureCategoryDir(ctx context.Context, logger *slog.Logger, category string) {
	if category == "" || h.localRoot == "" {
		return
	}

	dir := filepath.Join(h.localRoot, category)
	if err := os.MkdirAll(dir, categoryDirPerm); err != nil {
		logger.WarnContext(ctx, "failed to create category directory",
			"dir", dir, "category", category, "err", err)
	}
}

func (h *TransmissionHandler) handleTorrentRemove(ctx context.Context, req *TransmissionRequest) (*TransmissionResponse, error) {
	logger := logctx.LoggerFromContext(ctx)
	logger.DebugContext(ctx, "received torrent remove request")

	if err := h.dc.RemoveTransfers(ctx, req.Arguments.IDs, req.Arguments.DeleteLocalData); err != nil {
		return nil, fmt.Errorf("failed to remove transfers: %w", err)
	}

	return &TransmissionResponse{
		Result: "success",
	}, nil
}

func (h *TransmissionHandler) handleTorrentGet(ctx context.Context) (*TransmissionResponse, error) {
	logger := logctx.LoggerFromContext(ctx).With("method", "handle_torrent_get")

	logger.DebugContext(ctx, "fetching torrents from download client")

	transfers, err := h.dc.GetTaggedTorrents(ctx, h.label)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	torrentsCount := len(transfers)
	logger.DebugContext(ctx, "fetched torrents from download client", "count", torrentsCount)

	ledgerStatuses, err := h.ledgerStatuses(transfers)
	if err != nil {
		// Failing the call is deliberate: the alternative is guessing, and guessing
		// "complete" is exactly how a partial file gets imported.
		return nil, err
	}

	logger.DebugContext(ctx, "converting torrents to transmission format")

	transmissionTorrents := make([]TransmissionTorrent, torrentsCount)

	for i, transfer := range transfers {
		// Convert string ID to int64
		id, err := strconv.ParseInt(transfer.ID, 10, 64)
		if err != nil {
			logger.ErrorContext(ctx, "failed to parse transfer ID", "id", transfer.ID, "err", err)

			continue
		}

		status, errorString := seedboxStatus(ctx, logger, transfer)

		// The advertised path must be the path that was written. The *arr apps join
		// these two and look for the result on disk, with no branching on whether
		// the transfer is one file or a folder -- so the name has to come from the
		// files, never from the transfer name, which the seedbox may have renamed
		// or suffixed after the fact.
		name, derived := transfer.LocalName()
		if !derived {
			logger.WarnContext(ctx, "advertising the transfer name as a fallback: no local name could be derived",
				"transfer_id", transfer.ID,
				"transfer_name", transfer.Name,
				"file_count", len(transfer.Files))
		}

		// Advertise each transfer under its own category, so Sonarr and Radarr each
		// keep only their own. Both filter on exactly this: the label when the client
		// reports labels support, the directory otherwise.
		labels := []string{h.label}
		if transfer.Label != "" {
			labels = []string{transfer.Label}
		}

		transmissionTorrents[i] = TransmissionTorrent{
			ID:            id,
			HashString:    transfer.HashString(),
			Name:          name,
			DownloadDir:   filepath.Join(h.localRoot, transfer.LocalSubdir()),
			TotalSize:     transfer.Size,
			LeftUntilDone: transfer.Size - transfer.Downloaded,
			IsFinished: strings.ToLower(transfer.Status) == "completed" ||
				strings.ToLower(transfer.Status) == "seeding" ||
				strings.ToLower(transfer.Status) == "finished",
			ETA:                transfer.EstimatedTime,
			Status:             status,
			ErrorString:        errorString,
			DownloadedEver:     transfer.Downloaded,
			Labels:             labels,
			PeersConnected:     transfer.PeersConnected,
			PeersSendingToUs:   transfer.PeersSendingToUs,
			PeersGettingFromUs: transfer.PeersGettingFromUs,
			RateDownload:       transfer.DownloadSpeed,
			FileCount:          uint32(len(transfer.Files)),
			SeedRatioLimit:     1.0,
			SeedRatioMode:      1,
			SeedIdleLimit:      100,
			SeedIdleMode:       1,
		}

		h.holdUntilLocal(ctx, logger, &transmissionTorrents[i], transfer, ledgerStatuses[transfer.ID])
	}

	logger.DebugContext(ctx, "converted torrents to transmission format", "count", len(transmissionTorrents))

	jsonTorrents, err := json.Marshal(map[string]interface{}{
		"torrents": transmissionTorrents,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal torrents: %w", err)
	}

	return &TransmissionResponse{
		Result:    "success",
		Arguments: jsonTorrents,
	}, nil
}

// seedboxStatus maps the seedbox's own transfer status onto Transmission's, with
// the seedbox's error message when it reports one. It describes the transfer on
// the seedbox only; holdUntilLocal overrides it for content not yet on local disk.
func seedboxStatus(
	ctx context.Context, logger *slog.Logger, tr *transfer.Transfer,
) (TransmissionTorrentStatus, *string) {
	switch strings.ToLower(tr.Status) {
	case "downloading":
		return StatusDownload, nil // 4
	case "in_queue", "waiting":
		return StatusDownloadWait, nil // 3
	case "finishing", "checking":
		return StatusCheck, nil // 2
	case "completed", "finished", "seeding":
		return StatusSeed, nil // 6
	case "seedingwait":
		return StatusSeedWait, nil // 5
	case "error":
		if tr.ErrorMessage != "" {
			return StatusStopped, &tr.ErrorMessage // 0
		}

		return StatusStopped, nil // 0
	default:
		logger.WarnContext(ctx, "unknown put.io transfer status, defaulting to stopped",
			"status", tr.Status, "transfer_id", tr.ID)

		return StatusStopped, nil // 0
	}
}

// ledgerStatuses looks up every listed transfer in the ledger in one query. A
// transfer the ledger has no row for is absent from the result.
func (h *TransmissionHandler) ledgerStatuses(transfers []*transfer.Transfer) (map[string]string, error) {
	if h.ledger == nil {
		return map[string]string{}, nil
	}

	ids := make([]string, len(transfers))
	for i, t := range transfers {
		ids[i] = t.ID
	}

	statuses, err := h.ledger.TransferStatuses(ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read local download state: %w", err)
	}

	return statuses, nil
}

// holdUntilLocal keeps a transfer the seedbox has finished reported as still
// downloading until the downloader has finished writing it to local disk.
//
// Sonarr and Radarr import a Transmission torrent as soon as it reports nothing
// left to download (or isFinished) in a seeding/stopped state, and they import
// whatever is at the advertised path at that moment. Reporting the seedbox's
// state alone therefore handed them the local file while it was still being
// written -- or after its download had failed: Radarr imported 41.6 GB of a 50 GB
// file two seconds after the local copy died with an unexpected EOF.
//
// Only the ledger says a transfer is on local disk (storage.IsLocal). A transfer
// the ledger has no row for is treated as not local. That is correct, not merely
// cautious: the pipeline lists transfers with the same label as this handler and
// claims every one the seedbox has finished that has files, recording it in the
// ledger, so an unrecorded transfer is one it has not got to yet (or one recorded
// in a ledger that was since lost, which it downloads again). Transfers it skips
// for having no files are held too, but with an errorString saying why, so the
// *arr app shows a warning rather than a download that silently never ends.
// Everything the seedbox has not finished keeps its seedbox-derived state.
func (h *TransmissionHandler) holdUntilLocal(
	ctx context.Context, logger *slog.Logger, t *TransmissionTorrent, tr *transfer.Transfer, ledgerStatus string,
) {
	if !tr.IsAvailable() || storage.IsLocal(ledgerStatus) {
		return
	}

	// Never zero while held: zero left is itself how the *arr apps detect completion.
	left := max(tr.Size-h.localBytes(tr), 1)

	t.Status = StatusDownload
	t.IsFinished = false
	t.LeftUntilDone = left
	t.ErrorString = localHoldReason(tr, ledgerStatus)

	logger.DebugContext(ctx, "seedbox transfer complete but not yet on local disk, reporting it as downloading",
		"transfer_id", tr.ID, "ledger_status", ledgerStatus, "left_until_done", left)
}

// localHoldReason explains a hold that will not resolve by just waiting for the
// download in progress, or nil when that is all it takes. Sonarr and Radarr show
// a non-empty errorString as a warning on the queue item and do not import it.
func localHoldReason(tr *transfer.Transfer, ledgerStatus string) *string {
	var reason string

	switch {
	case ledgerStatus == storage.StatusMissing:
		reason = "transfer went missing from the seedbox before its local download finished"
	case ledgerStatus == storage.StatusFailed:
		reason = "local download failed; it is retried on the next poll"
	case !tr.IsDownloadable():
		reason = "the seedbox lists no files for this transfer, so there is nothing to download locally"
	default:
		return nil
	}

	return &reason
}

// localBytes is how much of a transfer's content is on local disk, for the
// progress shown while it is held. Best effort: files are written in place, so
// their size on disk is what has been written so far, and anything that cannot be
// stat'ed counts as nothing. It is only ever used for display -- completion is
// decided by the ledger, never by sizes on disk.
func (h *TransmissionHandler) localBytes(tr *transfer.Transfer) int64 {
	if h.localRoot == "" {
		return 0
	}

	var total int64

	for _, f := range tr.Files {
		info, err := os.Stat(filepath.Join(h.localRoot, tr.LocalSubdir(), f.Path))
		if err != nil || info.IsDir() {
			continue
		}

		total += min(info.Size(), f.Size)
	}

	return total
}

// formatTransmissionError converts internal errors to Transmission-compatible error messages.
// Transmission RPC uses the "result" field for error reporting - this function
// produces user-friendly error messages for common failure cases.
func formatTransmissionError(err error) string {
	var invalidErr *transfer.InvalidContentError
	if errors.As(err, &invalidErr) {
		return fmt.Sprintf("invalid torrent: %s", invalidErr.Reason)
	}

	var networkErr *transfer.NetworkError
	if errors.As(err, &networkErr) {
		return fmt.Sprintf("upload failed: %s", networkErr.APIMessage)
	}

	var dirErr *transfer.DirectoryError
	if errors.As(err, &dirErr) {
		return fmt.Sprintf("directory error: %s", dirErr.Reason)
	}

	var authErr *transfer.AuthenticationError
	if errors.As(err, &authErr) {
		return "authentication failed"
	}

	// Generic fallback for unknown errors
	return fmt.Sprintf("error: %v", err)
}
