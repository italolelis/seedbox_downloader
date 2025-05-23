package dc

import (
	"context"
	"io"
)

type Torrent struct {
	ID       string
	Label    string
	Name     string
	SavePath string
	Progress float64
	Files    []*File
}

type File struct {
	Path string
	Size int64
}

type DownloadClient interface {
	GetTaggedTorrents(ctx context.Context, label string) ([]*Torrent, error)
	GrabFile(ctx context.Context, file *File) (io.ReadCloser, error)
}
