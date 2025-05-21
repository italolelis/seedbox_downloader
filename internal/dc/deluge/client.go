package deluge

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"context"

	"github.com/italolelis/seedbox_downloader/internal/dc"
	"github.com/italolelis/seedbox_downloader/internal/downloader/progress"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
)

type Client struct {
	BaseURL    string
	APIPath    string
	Username   string
	Password   string
	httpClient *http.Client
	Insecure   bool   // skip TLS verification if true
	cookie     string // session cookie
}

type Torrent struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Name     string `json:"name"`
	FileName string `json:"file_name"`
	SavePath string `json:"save_path"`
}

func NewClient(baseURL, apiPath, username string, password string, insecure ...bool) *Client {
	client := &Client{
		BaseURL:    baseURL,
		APIPath:    apiPath,
		Username:   username,
		Password:   password,
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
	payload := map[string]interface{}{
		"id":     1,
		"method": "auth.login",
		"params": []interface{}{c.Password},
	}
	body, _ := json.Marshal(payload)
	logger.Debug("sending auth.login", "url", url)
	// Use http.NewRequest to set headers like requests does
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		logger.Error("request error", "err", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
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
		Result bool        `json:"result"`
		Error  interface{} `json:"error"`
		ID     int         `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		logger.Error("decode error", "err", err)
		return err
	}
	if !rpcResp.Result {
		logger.Error("login failed", "error", rpcResp.Error)
		return fmt.Errorf("deluge auth.login failed: %v", rpcResp.Error)
	}
	logger.Debug("success")
	return nil
}

// Ensure Client implements DownloadClient
var _ dc.DownloadClient = (*Client)(nil)

// Add a conversion method to DownloadClient.TorrentInfo
func (t Torrent) ToTorrentInfo() dc.TorrentInfo {
	return dc.TorrentInfo{
		ID:       t.ID,
		FileName: t.FileName,
		Label:    t.Label,
		SavePath: t.SavePath,
	}
}

// Update GetTaggedTorrents to match DownloadClient interface
func (c *Client) GetTaggedTorrents(ctx context.Context, tag string) ([]dc.TorrentInfo, error) {
	delugeTorrents, err := c.getTaggedTorrentsRaw(ctx, tag)
	if err != nil {
		return nil, err
	}
	var infos []dc.TorrentInfo
	for _, t := range delugeTorrents {
		infos = append(infos, t.ToTorrentInfo())
	}
	return infos, nil
}

// Move the original logic to a helper
func (c *Client) getTaggedTorrentsRaw(ctx context.Context, tag string) ([]Torrent, error) {
	logger := logctx.LoggerFromContext(ctx).With("tag", tag, "method", "core.get_torrents_status")

	url := fmt.Sprintf("%s%s", c.BaseURL, c.APIPath)
	payload := map[string]interface{}{
		"id":     2,
		"method": "core.get_torrents_status",
		"params": []interface{}{nil, []string{"name", "progress", "label", "save_path", "files", "hash"}},
	}
	body, _ := json.Marshal(payload)
	logger.Debug("sending core.get_torrents_status")

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		logger.Error("failed to create new request with context", "err", err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "_session_id", Value: c.cookie})
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to execute request to get torrents from deluge", "err", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		logger.Error("non-200 response", "status", resp.StatusCode, "body", string(b))
		return nil, fmt.Errorf("get_torrents_status request failed: %s", string(b))
	}

	var rpcResp struct {
		Result map[string]map[string]interface{} `json:"result"`
		Error  interface{}                       `json:"error"`
		ID     int                               `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		logger.Error("decode error", "err", err)
		return nil, err
	}
	if rpcResp.Error != nil {
		logger.Error("API error", "error", rpcResp.Error)
		return nil, fmt.Errorf("deluge core.get_torrents_status error: %v", rpcResp.Error)
	}

	var torrents []Torrent
	for id, fields := range rpcResp.Result {
		label, _ := fields["label"].(string)
		progress, _ := fields["progress"].(float64)
		if label == tag && progress == 100.0 {
			files, ok := fields["files"].([]interface{})
			if !ok {
				logger.Warn("Torrent has no files array", "torrent_id", id)
				continue
			}
			savePath, _ := fields["save_path"].(string)
			for _, f := range files {
				fileInfo, ok := f.(map[string]interface{})
				if !ok {
					continue
				}
				filePath, _ := fileInfo["path"].(string)
				if filePath == "" {
					continue
				}
				torrents = append(torrents, Torrent{
					ID:       id,
					Label:    label,
					Name:     fields["name"].(string),
					FileName: filePath,
					SavePath: savePath,
				})
			}
		}
	}
	logger.Debug("found files to download",
		"method", "core.get_torrents_status",
		"count", len(torrents),
	)
	return torrents, nil
}

// DownloadFile implements DownloadClient.DownloadFile for Deluge
func (c *Client) DownloadFile(ctx context.Context, torrent dc.TorrentInfo, targetPath string) error {
	logger := logctx.LoggerFromContext(ctx)
	filePathEscaped := torrent.SavePath
	if !strings.HasSuffix(filePathEscaped, "/") && filePathEscaped != "" {
		filePathEscaped += "/"
	}
	filePathEscaped += torrent.FileName
	filePathEscaped = strings.TrimPrefix(filePathEscaped, "/")
	url := fmt.Sprintf("%s/%s", strings.TrimRight(c.BaseURL, "/"), filePathEscaped)

	client := c.httpClient
	if c.Insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client = &http.Client{Transport: tr}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		logger.Error("failed to create HTTP request", "url", url, "err", err)
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "_session_id", Value: c.cookie})
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("failed to download file", "url", url, "err", err)
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("failed to download file, bad status", "url", url, "status", resp.Status)
		return fmt.Errorf("failed to download file: %s", resp.Status)
	}

	if err = os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		logger.Error("failed to create target directory", "dir", filepath.Dir(targetPath), "err", err)
		return fmt.Errorf("failed to create target directory: %w", err)
	}
	out, err := os.Create(targetPath)
	if err != nil {
		logger.Error("failed to create target file", "target", targetPath, "err", err)
		return fmt.Errorf("failed to create target file: %w", err)
	}
	defer out.Close()

	totalBytes := resp.ContentLength
	progressInterval := int64(5 * 1024 * 1024) // 5MB
	progressCb := func(written int64, total int64) {
		if total > 0 {
			logger.Info("Download progress", "url", url, "target", targetPath, "downloaded", written, "total", total, "percent", float64(written)*100/float64(total))
		} else {
			logger.Info("Download progress", "url", url, "target", targetPath, "downloaded", written)
		}
	}
	pr := progress.NewReader(resp.Body, totalBytes, progressInterval, progressCb)

	_, err = io.Copy(out, pr)
	if err != nil {
		logger.Error("failed to copy file contents", "url", url, "target", targetPath, "err", err)
		return fmt.Errorf("failed to copy file: %w", err)
	}
	logger.Info("downloaded and saved file", "url", url, "target", targetPath)
	return nil
}
