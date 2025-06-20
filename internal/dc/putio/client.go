package putio

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/putdotio/go-putio"
	"golang.org/x/oauth2"
)

type Client struct {
	putioClient *putio.Client
}

func NewClient(token string, insecure ...bool) *Client {
	client := &Client{}

	// Initialize Put.io client
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauthClient := oauth2.NewClient(context.Background(), tokenSource)
	client.putioClient = putio.NewClient(oauthClient)

	return client
}

// Update GetTaggedTorrents to match DownloadClient interface.
func (c *Client) GetTaggedTorrents(ctx context.Context, tag string) ([]*transfer.Transfer, error) {
	logger := logctx.LoggerFromContext(ctx).With("tag", tag)

	transfers, err := c.putioClient.Transfers.List(ctx)
	if err != nil {
		logger.Error("failed to get transfers", "err", err)

		return nil, fmt.Errorf("failed to get transfers: %w", err)
	}

	torrents := make([]*transfer.Transfer, 0, len(transfers))

	for _, t := range transfers {
		if t.FileID == 0 {
			logger.Debug("skipping transfer because it's not a downloadable transfer", "transfer_id", t.ID, "status", t.Status)

			continue
		}

		file, err := c.putioClient.Files.Get(ctx, t.FileID)
		if err != nil {
			logger.Error("failed to get file", "transfer_id", t.ID, "err", err)

			continue
		}

		parent, err := c.putioClient.Files.Get(ctx, file.ParentID)
		if err != nil {
			logger.Error("failed to get parent file", "file_id", file.ID, "err", err)

			continue
		}

		if parent.IsDir() && parent.Name != tag {
			logger.Debug("skipping file", "file_id", file.ID, "file_name", file.Name, "parent_name", parent.Name)

			continue
		}

		// Convert Put.io transfer to our Torrent type
		torrent := &transfer.Transfer{
			ID:                 fmt.Sprintf("%d", t.ID),
			Name:               t.Name,
			Label:              tag,
			Progress:           float64(t.PercentDone),
			Files:              make([]*transfer.File, 0),
			Size:               int64(t.Size),
			Source:             t.Source,
			Status:             t.Status,
			EstimatedTime:      t.EstimatedTime,
			SavePath:           "/" + tag,
			PeersConnected:     int64(t.PeersConnected),
			PeersGettingFromUs: int64(t.PeersGettingFromUs),
			PeersSendingToUs:   int64(t.PeersSendingToUs),
			Downloaded:         int64(t.Downloaded),
		}

		files, err := c.getFilesRecursively(ctx, file.ID, file.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get files for transfer: %w", err)
		}

		torrent.Files = append(torrent.Files, files...)

		torrents = append(torrents, torrent)
	}

	logger.Debug("found torrents to download", "torrent_count", len(torrents))

	return torrents, nil
}

// GrabFile implements DownloadClient.GrabFile for Put.io.
func (c *Client) GrabFile(ctx context.Context, file *transfer.File) (io.ReadCloser, error) {
	logger := logctx.LoggerFromContext(ctx)

	url, err := c.putioClient.Files.URL(ctx, file.ID, false)
	if err != nil {
		logger.Error("failed to get file download url", "file_id", file.ID, "err", err)

		return nil, fmt.Errorf("failed to get file download url: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		logger.Error("failed to get file", "file_id", file.ID, "err", err)

		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	return resp.Body, nil
}

func (c *Client) Authenticate(ctx context.Context) error {
	logger := logctx.LoggerFromContext(ctx)

	logger.Info("authenticating with Put.io")

	user, err := c.putioClient.Account.Info(ctx)
	if err != nil {
		logger.Error("failed to get account info", "err", err)

		return fmt.Errorf("failed to get account info: %w", err)
	}

	logger.Info("authenticated with Put.io", "user", user.Username)

	return nil
}

func (c *Client) AddTransfer(ctx context.Context, url string, downloadDir string) (*transfer.Transfer, error) {
	logger := logctx.LoggerFromContext(ctx).With("download_dir", downloadDir)

	var dirID int64

	if downloadDir != "" {
		var err error

		dirID, err = c.findDirectoryID(ctx, downloadDir)
		if err != nil {
			return nil, fmt.Errorf("failed to find directory: %w", err)
		}
	}

	logger.Info("adding transfer to Put.io", "transfer_url", url)

	t, err := c.putioClient.Transfers.Add(ctx, url, dirID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to add transfer: %w", err)
	}

	logger.Info("transfer added to Put.io", "transfer_id", t.ID)

	return &transfer.Transfer{
		ID:                 fmt.Sprintf("%d", t.ID),
		Name:               t.Name,
		Downloaded:         t.Downloaded,
		Size:               int64(t.Size),
		EstimatedTime:      t.EstimatedTime,
		Status:             t.Status,
		Progress:           float64(t.PercentDone),
		Files:              make([]*transfer.File, 0),
		Source:             t.Source,
		PeersConnected:     int64(t.PeersConnected),
		PeersGettingFromUs: int64(t.PeersGettingFromUs),
	}, nil
}

// RemoveTransfers implements DownloadClient.RemoveTransfers for Put.io. The transferIDs are the hashes of the transfers.
func (c *Client) RemoveTransfers(ctx context.Context, transferIDs []string, deleteFiles bool) error {
	logger := logctx.LoggerFromContext(ctx)

	logger.Info("removing transfer from Put.io", "transfer_ids", transferIDs)

	transfers, err := c.putioClient.Transfers.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to get transfers: %w", err)
	}

	putioTransfers := c.filterMatchingTransferIds(transfers, transferIDs)

	if len(putioTransfers) == 0 {
		return fmt.Errorf("transfer not found: %v", transferIDs)
	}

	for _, transfer := range putioTransfers {
		if err := c.putioClient.Transfers.Cancel(ctx, transfer.ID); err != nil {
			return fmt.Errorf("failed to remove transfer: %w", err)
		}

		// If deleteLocalData is true and the file exists, delete it
		if deleteFiles && transfer.FileID != 0 {
			logger.Info("deleting local file data", "file_id", transfer.FileID)

			if err := c.putioClient.Files.Delete(ctx, transfer.FileID); err != nil {
				return fmt.Errorf("failed to delete local file data: %w", err)
			}

			logger.Info("local file data deleted", "file_id", transfer.FileID)
		}
	}

	return nil
}

func (c *Client) filterMatchingTransferIds(transfers []putio.Transfer, transferIDs []string) []putio.Transfer {
	matchingTransfers := make([]putio.Transfer, 0, len(transferIDs))

	for _, t := range transfers {
		hash := sha1.Sum([]byte(fmt.Sprintf("%d", t.ID)))
		hashString := hex.EncodeToString(hash[:])

		if slices.Contains(transferIDs, hashString) {
			matchingTransfers = append(matchingTransfers, t)
		}
	}

	return matchingTransfers
}

func (c *Client) findDirectoryID(ctx context.Context, downloadDir string) (int64, error) {
	search, err := c.putioClient.Files.Search(ctx, downloadDir, 1)
	if err != nil {
		return 0, fmt.Errorf("error searching for directory: %w", err)
	}

	if len(search.Files) == 0 {
		return 0, fmt.Errorf("directory not found: %s", downloadDir)
	}

	if !search.Files[0].IsDir() {
		return 0, fmt.Errorf("search result is not a directory: %s", downloadDir)
	}

	return search.Files[0].ID, nil
}

func (c *Client) getFilesRecursively(ctx context.Context, parentID int64, basePath string) ([]*transfer.File, error) {
	logger := logctx.LoggerFromContext(ctx).With("parent_id", parentID, "base_path", basePath)

	var result []*transfer.File

	file, err := c.putioClient.Files.Get(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	if !file.IsDir() {
		result = append(result, &transfer.File{
			ID:   file.ID,
			Path: filepath.Join(basePath, file.Name),
			Size: file.Size,
		})

		return result, nil
	}

	files, _, err := c.putioClient.Files.List(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	for _, f := range files {
		switch strings.ToLower(f.FileType) {
		case "file", "text", "video", "audio", "archive":
			result = append(result, &transfer.File{
				ID:   f.ID,
				Path: filepath.Join(basePath, f.Name),
				Size: f.Size,
			})
		case "folder":
			nestedFiles, err := c.getFilesRecursively(ctx, f.ID, filepath.Join(basePath, f.Name))
			if err != nil {
				logger.Error("failed to get nested files", "err", err)

				continue
			}

			result = append(result, nestedFiles...)
		}
	}

	return result, nil
}
