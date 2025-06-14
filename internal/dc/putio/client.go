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

	"github.com/italolelis/seedbox_downloader/internal/dc"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
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
func (c *Client) GetTaggedTorrents(ctx context.Context, tag string) ([]*dc.Torrent, error) {
	logger := logctx.LoggerFromContext(ctx).With("tag", tag)

	transfers, err := c.putioClient.Transfers.List(ctx)
	if err != nil {
		logger.Error("failed to get transfers", "err", err)

		return nil, fmt.Errorf("failed to get transfers: %w", err)
	}

	torrents := make([]*dc.Torrent, 0, len(transfers))

	for _, transfer := range transfers {
		status := strings.ToLower(transfer.Status)
		if status != "completed" && status != "seeding" && status != "seedingwait" && status != "finished" {
			continue
		}

		file, err := c.putioClient.Files.Get(ctx, transfer.FileID)
		if err != nil {
			logger.Error("failed to get file", "transfer_id", transfer.ID, "err", err)

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
		torrent := &dc.Torrent{
			ID:                 fmt.Sprintf("%d", transfer.ID),
			Name:               transfer.Name,
			Label:              tag,
			Progress:           float64(transfer.PercentDone),
			Files:              make([]*dc.File, 0),
			Size:               int64(transfer.Size),
			Source:             transfer.Source,
			Status:             transfer.Status,
			EstimatedTime:      transfer.EstimatedTime,
			SavePath:           "/" + tag,
			PeersConnected:     int64(transfer.PeersConnected),
			PeersGettingFromUs: int64(transfer.PeersGettingFromUs),
			PeersSendingToUs:   int64(transfer.PeersSendingToUs),
			Downloaded:         int64(transfer.Downloaded),
		}

		switch strings.ToLower(file.FileType) {
		case "file", "video":
			torrent.Files = append(torrent.Files, &dc.File{
				ID:   file.ID,
				Path: file.Name, // Use Name instead of Path
				Size: file.Size,
			})
		case "folder":
			// Get files for this transfer
			files, _, err := c.putioClient.Files.List(ctx, transfer.FileID)
			if err != nil {
				logger.Error("failed to get files for transfer", "transfer_id", transfer.ID, "err", err)

				continue
			}

			for _, f := range files {
				torrent.Files = append(torrent.Files, &dc.File{
					ID:   f.ID,
					Path: filepath.Join(file.Name, f.Name),
					Size: f.Size,
				})
			}
		}

		torrents = append(torrents, torrent)
	}

	logger.Debug("found torrents to download", "torrent_count", len(torrents))

	return torrents, nil
}

// GrabFile implements DownloadClient.GrabFile for Put.io.
func (c *Client) GrabFile(ctx context.Context, file *dc.File) (io.ReadCloser, error) {
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

func (c *Client) AddTransfer(ctx context.Context, url string, downloadDir string) (*dc.Torrent, error) {
	logger := logctx.LoggerFromContext(ctx)

	search, err := c.putioClient.Files.Search(ctx, downloadDir, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to search for directory: %w", err)
	}

	if len(search.Files) == 0 {
		return nil, fmt.Errorf("directory not found: %s", downloadDir)
	}

	if !search.Files[0].IsDir() {
		return nil, fmt.Errorf("search result is not a directory: %s", downloadDir)
	}

	dirID := search.Files[0].ID

	logger.Info("adding transfer to Put.io", "transfer_url", url, "download_dir", downloadDir)

	t, err := c.putioClient.Transfers.Add(ctx, url, dirID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to add transfer: %w", err)
	}

	logger.Info("transfer added to Put.io", "transfer_id", t.ID)

	return &dc.Torrent{
		ID:                 fmt.Sprintf("%d", t.ID),
		Name:               t.Name,
		Downloaded:         t.Downloaded,
		Size:               int64(t.Size),
		EstimatedTime:      t.EstimatedTime,
		Status:             t.Status,
		Progress:           float64(t.PercentDone),
		Files:              make([]*dc.File, 0),
		Source:             t.Source,
		PeersConnected:     int64(t.PeersConnected),
		PeersGettingFromUs: int64(t.PeersGettingFromUs),
	}, nil
}

func (c *Client) RemoveTransfers(ctx context.Context, transferIDs []string, deleteLocalData bool) error {
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
		// Cancel the transfer
		if err := c.putioClient.Transfers.Cancel(ctx, transfer.ID); err != nil {
			return fmt.Errorf("failed to remove transfer: %w", err)
		}

		// If deleteLocalData is true and the file exists, delete it
		if deleteLocalData && transfer.FileID != 0 {
			logger.Info("deleting local file data", "file_id", transfer.FileID)

			if err := c.putioClient.Files.Delete(ctx, transfer.FileID); err != nil {
				return fmt.Errorf("failed to delete local file data: %w", err)
			}
		}

		logger.Info("transfer removed from Put.io", "transfer_id", transfer.ID)
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
