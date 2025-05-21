package download_client

import (
	"context"
)

type TorrentInfo struct {
	ID       string
	FileName string
	Label    string
	SavePath string
}

type DownloadClient interface {
	GetTaggedTorrents(ctx context.Context, label string) ([]TorrentInfo, error)
	DownloadFile(ctx context.Context, torrent TorrentInfo, targetPath string) error
}
