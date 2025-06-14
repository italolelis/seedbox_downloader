package dc

import (
	"context"
	"io"
)

type Torrent struct {
	ID                 string
	Label              string
	Name               string
	SavePath           string
	Progress           float64
	Downloaded         int64
	ErrorMessage       string
	EstimatedTime      int64
	PeersConnected     int64
	PeersGettingFromUs int64
	PeersSendingToUs   int64
	SecondsSeeding     int64
	Size               int64
	Source             string
	Status             string
	Files              []*File
}

type File struct {
	ID   int64
	Path string
	Size int64
}

type DownloadClient interface {
	Authenticate(ctx context.Context) error
	GetTaggedTorrents(ctx context.Context, label string) ([]*Torrent, error)
	GrabFile(ctx context.Context, file *File) (io.ReadCloser, error)
}
