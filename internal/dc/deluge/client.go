package deluge

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"context"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	defaultTimeout = 10 * time.Second
)

type Client struct {
	BaseURL      string
	APIPath      string
	CompletedDir string
	Username     string
	Password     string
	httpClient   *http.Client
	Insecure     bool   // skip TLS verification if true
	cookie       string // session cookie
}

type DelugeResponse struct {
	Result map[string]*Torrent `json:"result"`
	Error  any                 `json:"error"`
	ID     int                 `json:"id"`
}

type Torrent struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Name     string  `json:"name"`
	SavePath string  `json:"save_path"`
	Progress float64 `json:"progress"`
	Files    []File  `json:"files"`
}

type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func NewClient(baseURL, apiPath, completedDir, username string, password string, insecure ...bool) *Client {
	client := &Client{
		BaseURL:      baseURL,
		APIPath:      apiPath,
		CompletedDir: completedDir,
		Username:     username,
		Password:     password,
		httpClient:   &http.Client{Timeout: defaultTimeout},
	}

	if len(insecure) > 0 && insecure[0] {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.httpClient.Transport = tr
		client.Insecure = true
	}

	return client
}

func (c *Client) Authenticate(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx).With("method", "auth.login")

	url := fmt.Sprintf("%s%s", c.BaseURL, c.APIPath)
	payload := map[string]any{
		"id":     1,
		"method": "auth.login",
		"params": []any{c.Password},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal payload", "err", err)

		return err
	}

	logger.Info("authenticating with deluge", "username", c.Username)

	// Use http.NewRequest to set headers like requests does
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		logger.Error("request error", "err", err)

		return err
	}

	req.Header.Set("Content-Type", "application/json")

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("HTTP error", "err", err)

		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		logger.Error("non-200 response", "status", resp.StatusCode, "body", string(b))

		return fmt.Errorf("auth failed: %s", string(b))
	}

	// Save session cookie (like requests.Session)
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "_session_id" {
			c.cookie = cookie.Value
		}
	}

	var rpcResp struct {
		Result bool `json:"result"`
		Error  any  `json:"error"`
		ID     int  `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		logger.Error("decode error", "err", err)

		return err
	}

	if !rpcResp.Result {
		logger.Error("login failed", "error", rpcResp.Error)

		return fmt.Errorf("deluge auth.login failed: %v", rpcResp.Error)
	}

	logger.Debug("authenticated with deluge")

	return nil
}

// Add a conversion method to DownloadClient.Torrent.
func (t *Torrent) ToTorrent() *transfer.Transfer {
	files := make([]*transfer.File, 0, len(t.Files))
	for _, f := range t.Files {
		files = append(files, &transfer.File{
			Path: f.Path,
			Size: f.Size,
		})
	}

	return &transfer.Transfer{
		ID:       t.ID,
		Name:     t.Name,
		Label:    t.Label,
		SavePath: t.SavePath,
		Progress: t.Progress,
		Files:    files,
	}
}

// Update GetTaggedTorrents to match DownloadClient interface.
func (c *Client) GetTaggedTorrents(ctx context.Context, tag string) ([]*transfer.Transfer, error) {
	delugeTorrents, err := c.getTaggedTorrentsRaw(ctx, tag)
	if err != nil {
		return nil, err
	}

	infos := make([]*transfer.Transfer, 0, len(delugeTorrents))

	for _, t := range delugeTorrents {
		infos = append(infos, t.ToTorrent())
	}

	return infos, nil
}

// GrabFile implements DownloadClient.GrabFile for Deluge.
func (c *Client) GrabFile(ctx context.Context, file *transfer.File) (io.ReadCloser, error) {
	logger := logctx.LoggerFromContext(ctx)

	req, url, err := c.buildDownloadRequest(ctx, file)
	if err != nil {
		logger.Error("failed to create HTTP request", "url", url, "err", err)

		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	client := c.httpClient

	if c.Insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("failed to download file", "url", url, "err", err)

		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Error("failed to download file, bad status", "url", url, "status", resp.Status)

		resp.Body.Close()

		return nil, fmt.Errorf("failed to download file: %s", resp.Status)
	}

	return resp.Body, nil
}

// Helper function for original logic.
func (c *Client) getTaggedTorrentsRaw(ctx context.Context, tag string) ([]*Torrent, error) {
	logger := logctx.LoggerFromContext(ctx).With("tag", tag, "method", "core.get_torrents_status")

	url := fmt.Sprintf("%s%s", c.BaseURL, c.APIPath)
	payload := map[string]any{
		"id":     2,
		"method": "core.get_torrents_status",
		"params": []any{nil, []string{"name", "progress", "label", "save_path", "files", "hash"}},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		logger.Error("failed to create request", "err", err)

		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "_session_id", Value: c.cookie})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("request execution failed", "err", err)

		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		logger.Error("non-200 response", "status", resp.StatusCode, "body", string(b))

		return nil, fmt.Errorf("request failed: %s", string(b))
	}

	var delugeResp DelugeResponse

	if err := json.NewDecoder(resp.Body).Decode(&delugeResp); err != nil {
		logger.Error("decode error", "err", err)

		return nil, err
	}

	if delugeResp.Error != nil {
		logger.Error("API error", "error", delugeResp.Error)

		return nil, fmt.Errorf("API error: %v", delugeResp.Error)
	}

	var torrents []*Torrent

	for id, torrent := range delugeResp.Result {
		torrent.ID = id

		if torrent.Label == tag && torrent.Progress == 100 && len(torrent.Files) > 0 {
			torrents = append(torrents, torrent)
		}
	}

	logger.Debug("found torrents to download", "torrent_count", len(torrents))

	return torrents, nil
}

func (c *Client) buildDownloadRequest(ctx context.Context, file *transfer.File) (*http.Request, string, error) {
	url := fmt.Sprintf("%s%s/%s", strings.TrimRight(c.BaseURL, "/"), strings.TrimRight(c.CompletedDir, "/"), file.Path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, url, err
	}

	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "_session_id", Value: c.cookie})
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	return req, url, nil
}
